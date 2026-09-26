package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"notes-bot/frontends/telegram/bot"
	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tghandlers"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/grpcutil"
	"notes-bot/internal/telemetry"
)

// maxConcurrentUpdates limits how many updates are processed in parallel.
// Each update may issue gRPC calls, LLM calls, and Telegram API requests;
// unbounded goroutines on a backlog (e.g. after bot restart) can exhaust
// file descriptors or cause OOM.
const maxConcurrentUpdates = 16

type telegramTracingTransport struct {
	base     http.RoundTripper
	botToken string
}

func (t telegramTracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := otel.Tracer("telegram").Start(req.Context(), "Telegram Bot API "+req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.request.method", req.Method),
			attribute.String("server.address", req.URL.Hostname()),
			attribute.String("url.full", maskedTelegramAPIURL(req.URL, t.botToken)),
		),
	)
	defer span.End()

	// The request keeps the real token; only trace attributes are redacted.
	outgoing := req.Clone(ctx)
	outgoing.Header = req.Header.Clone()
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outgoing.Header))
	resp, err := t.base.RoundTrip(outgoing)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	return resp, nil
}

func maskedTelegramAPIURL(rawURL *url.URL, botToken string) string {
	sanitized := *rawURL
	sanitized.Path = strings.Replace(sanitized.Path, "/bot"+botToken, "/botREDACTED", 1)
	sanitized.RawPath = ""
	sanitized.RawQuery = ""
	sanitized.ForceQuery = false
	return sanitized.String()
}

var logger *zap.Logger

func init() {
	logger = applog.New("notes-bot-telegram",
		os.Getenv("BOT_TOKEN"),
		os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
	)
	tgstates.SetLogger(logger)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatal("invalid config", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "telegram")
	if err != nil {
		logger.Fatal("failed to init tracer", zap.Error(err))
	}
	defer shutdown(context.Background()) //nolint:errcheck

	metricsHandler, metricsShutdown, err := telemetry.InitMetrics()
	if err != nil {
		logger.Fatal("failed to init metrics", zap.Error(err))
	}
	defer metricsShutdown()
	bot.InitTelegramMetrics()

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9102"
	}
	grpcutil.StartMetricsServer(logger, metricsPort, metricsHandler)

	// Clients
	coreClient, err := clients.NewCoreClient(cfg.CoreGRPCHost, cfg.CoreGRPCPort)
	if err != nil {
		logger.Fatal("failed to create core client", zap.Error(err))
	}
	defer coreClient.Close()

	notifClient, err := clients.NewNotificationsClient(cfg.NotificationsGRPCHost, cfg.NotificationsGRPCPort)
	if err != nil {
		logger.Fatal("failed to create notifications client", zap.Error(err))
	}
	defer notifClient.Close()

	whisperClient, err := clients.NewWhisperClient(cfg.WhisperGRPCHost, cfg.WhisperGRPCPort)
	if err != nil {
		logger.Fatal("failed to create whisper client", zap.Error(err))
	}
	defer whisperClient.Close()

	searchClient, err := clients.NewSearchClient(cfg.SearchGRPCHost, cfg.SearchGRPCPort)
	if err != nil {
		logger.Fatal("failed to create search client", zap.Error(err))
	}
	defer searchClient.Close()

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})
	defer rdb.Close()
	if err := redisotel.InstrumentTracing(rdb); err != nil {
		logger.Fatal("failed to instrument redis", zap.Error(err))
	}

	stateManager := tgstates.NewStateManager(rdb, cfg.TimezoneOffsetHours, cfg.DayStartHour)

	llmClient := clients.NewLLMClient(cfg.LLMHost, cfg.LLMPort, cfg.LLMModel)

	locationClient := clients.NewLocationClient(cfg.LocationHost, cfg.LocationPort)

	app := &tghandlers.App{
		Cfg:           cfg,
		Core:          coreClient,
		Notifications: notifClient,
		Whisper:       whisperClient,
		Search:        searchClient,
		LLM:           llmClient,
		Location:      locationClient,
		State:         stateManager,
		Logger:        logger,
	}

	// Telegram bot
	httpClient := &http.Client{
		Transport: telegramTracingTransport{
			base: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DialContext:         (&net.Dialer{Timeout: 60 * time.Second}).DialContext,
				TLSHandshakeTimeout: 60 * time.Second,
			},
			botToken: cfg.BOTToken,
		},
		Timeout: 120 * time.Second,
	}
	tgBot, err := newTelegramBot(cfg, httpClient, logger)
	if err != nil {
		logger.Fatal("failed to create telegram bot", zap.Error(err))
	}
	me, err := tgBot.GetMe(ctx)
	if err != nil {
		logger.Fatal("failed to authorize telegram bot", zap.Error(err))
	}
	logger.Info("bot authorized", zap.String("username", me.Username))

	// Start Kafka consumer in background.
	// Offsets are committed to Kafka via consumer group — no external store needed.
	var wg sync.WaitGroup
	wg.Go(func() {
		bot.RunKafkaConsumer(ctx, cfg.KafkaBootstrapServers, app.MakeReminderHandler(tgBot), logger)
	})

	if cfg.WebhookURL != "" {
		runWebhook(ctx, cfg, tgBot, app, &wg, logger)
	} else {
		runPolling(ctx, tgBot, app, &wg, logger)
	}
}

func newTelegramBot(cfg *config.Config, client *http.Client, log *zap.Logger) (*telego.Bot, error) {
	options := []telego.BotOption{
		telego.WithAPICaller(telegramAPICaller{caller: telegoapi.HTTPCaller{Client: client}, token: cfg.BOTToken}),
		telego.WithLogger(telegramLogger{log}),
	}
	if cfg.LocalAPIURL != "" {
		options = append(options, telego.WithAPIServer(strings.TrimSuffix(cfg.LocalAPIURL, "/")))
	}
	return telego.NewBot(cfg.BOTToken, options...)
}

// net/http includes the request URL in errors. Redact it before telego logs
// the error or a handler records it in a span, preserving errors.Is semantics.
type telegramAPICaller struct {
	caller telegoapi.Caller
	token  string
}

type telegramAPIError struct {
	err     error
	message string
}

func (e telegramAPIError) Error() string { return e.message }
func (e telegramAPIError) Unwrap() error { return e.err }

func (c telegramAPICaller) Call(ctx context.Context, endpoint string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	response, err := c.caller.Call(ctx, endpoint, data)
	if err != nil {
		return nil, telegramAPIError{err: err, message: strings.ReplaceAll(err.Error(), c.token, "REDACTED")}
	}
	return response, nil
}

func handleUpdateTraced(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
	updateType, userID := classifyUpdate(update)
	ctx, span := otel.Tracer("telegram").Start(ctx, "telegram.update "+updateType,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("telegram.update_type", updateType),
			attribute.Int64("telegram.user_id", userID),
		),
	)
	defer span.End()

	bot.UpdatesTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("type", updateType)))

	start := time.Now()
	handleUpdate(ctx, app, tgBot, update)
	bot.HandlerDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(attribute.String("type", updateType)),
	)
}

func classifyUpdate(update *telego.Update) (updateType string, userID int64) {
	switch {
	case messageCommand(update.Message) != "":
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		return "command", userID
	case update.Message != nil && (update.Message.Voice != nil || update.Message.VideoNote != nil):
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		return "voice", userID
	case update.Message != nil && update.Message.Location != nil:
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		return "location", userID
	case update.EditedMessage != nil && update.EditedMessage.Location != nil:
		if update.EditedMessage.From != nil {
			userID = update.EditedMessage.From.ID
		}
		return "location", userID
	case update.Message != nil:
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}
		return "text", userID
	case update.CallbackQuery != nil:
		if update.CallbackQuery.From.ID != 0 {
			userID = update.CallbackQuery.From.ID
		}
		return "callback", userID
	default:
		return "unknown", 0
	}
}

// Only a bot_command entity at the beginning of the message is a command.
func messageCommand(message *telego.Message) string {
	if message == nil || len(message.Entities) == 0 || message.Entities[0].Type != "bot_command" || message.Entities[0].Offset != 0 {
		return ""
	}
	command, _, _ := tu.ParseCommand(message.Text)
	return command
}

// Debug requests/responses may contain note content. Keep them out of logs.
type telegramLogger struct{ log *zap.Logger }

func (l telegramLogger) Debugf(string, ...any) {}
func (l telegramLogger) Errorf(format string, args ...any) {
	l.log.Error(fmt.Sprintf(format, args...))
}

type updateHandler func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update)

var commandHandlers = map[string]updateHandler{
	"start": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		app.HandleStart(ctx, tgBot, update)
	},
}

var updateHandlers = map[string]updateHandler{
	"command": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		if h, ok := commandHandlers[messageCommand(update.Message)]; ok {
			h(ctx, app, tgBot, update)
		}
	},
	"voice": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		app.HandleVoiceMessage(ctx, tgBot, update)
	},
	"location": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		app.HandleLocationMessage(ctx, tgBot, update)
	},
	"text": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		app.HandleTextMessage(ctx, tgBot, update)
	},
	"callback": func(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
		app.HandleCallback(ctx, tgBot, update)
	},
}

func handleUpdate(ctx context.Context, app *tghandlers.App, tgBot *telego.Bot, update *telego.Update) {
	updateType, userID := classifyUpdate(update)
	// Serialize full handler execution per user: handlers follow a
	// snapshot → decision → write pattern, so two concurrent updates from
	// the same user could otherwise act on stale state.
	if userID != 0 {
		unlock := app.LockUser(userID)
		defer unlock()
	}
	if h, ok := updateHandlers[updateType]; ok {
		h(ctx, app, tgBot, update)
	}
}

func runPolling(ctx context.Context, tgBot *telego.Bot, app *tghandlers.App, wg *sync.WaitGroup, log *zap.Logger) {
	updates, err := tgBot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{Timeout: 60})
	if err != nil {
		log.Error("failed to start polling", zap.Error(err))
		return
	}

	sem := semaphore.NewWeighted(maxConcurrentUpdates)
	var handlers sync.WaitGroup
	defer wg.Wait()
	defer handlers.Wait()

	log.Info("bot started (polling mode)")

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down bot")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := sem.Acquire(ctx, 1); err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer sem.Release(1)
				handleUpdateTraced(ctx, app, tgBot, &update)
			}()
		}
	}
}

func runWebhook(ctx context.Context, cfg *config.Config, tgBot *telego.Bot, app *tghandlers.App, wg *sync.WaitGroup, log *zap.Logger) {
	// Config.Validate checks the URL and secret before any clients are created.
	parsedURL, _ := url.Parse(cfg.WebhookURL)

	if err := tgBot.SetWebhook(ctx, &telego.SetWebhookParams{
		URL:         parsedURL.String(),
		SecretToken: cfg.WebhookSecret,
	}); err != nil {
		log.Fatal("failed to set webhook", zap.Error(err))
	}
	log.Info("webhook registered", zap.String("host", parsedURL.Host), zap.String("path", parsedURL.Path))

	path := parsedURL.Path
	if path == "" {
		path = "/"
	}
	updates := make(chan telego.Update, 100)
	mux := http.NewServeMux()
	mux.Handle(path, telegramWebhookHandler(cfg.WebhookSecret, updates))

	srv := &http.Server{
		Addr:              cfg.WebhookListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("webhook server error", zap.Error(err))
		}
	}()
	log.Info("bot started (webhook mode)", zap.String("addr", cfg.WebhookListenAddr))

	sem := semaphore.NewWeighted(maxConcurrentUpdates)
	var handlers sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down bot")

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tgBot.DeleteWebhook(shutdownCtx, &telego.DeleteWebhookParams{DropPendingUpdates: false}); err != nil {
				log.Warn("failed to delete webhook", zap.Error(err))
			}
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Warn("webhook server shutdown error", zap.Error(err))
			}

			handlers.Wait()
			wg.Wait()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := sem.Acquire(ctx, 1); err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer sem.Release(1)
				handleUpdateTraced(ctx, app, tgBot, &update)
			}()
		}
	}
}

func telegramWebhookHandler(secret string, updates chan<- telego.Update) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provided := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()
		var update telego.Update
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&update); err != nil {
			http.Error(w, "invalid update", http.StatusBadRequest)
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid update", http.StatusBadRequest)
			return
		}
		select {
		case updates <- update:
		case <-r.Context().Done():
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
