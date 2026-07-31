package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

var logger *zap.Logger

func init() {
	logger = applog.New()
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
		Transport: otelhttp.NewTransport(&http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         (&net.Dialer{Timeout: 60 * time.Second}).DialContext,
			TLSHandshakeTimeout: 60 * time.Second,
		}),
		Timeout: 120 * time.Second,
	}
	tgBot, err := tgbotapi.NewBotAPIWithClient(cfg.BOTToken, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		logger.Fatal("failed to create telegram bot", zap.Error(err))
	}
	logger.Info("bot authorized", zap.String("username", tgBot.Self.UserName))

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

func handleUpdateTraced(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
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

func classifyUpdate(update *tgbotapi.Update) (updateType string, userID int64) {
	switch {
	case update.Message != nil && update.Message.IsCommand():
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

type updateHandler func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update)

var commandHandlers = map[string]updateHandler{
	"start": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleStart(ctx, tgBot, update)
	},
}

var updateHandlers = map[string]updateHandler{
	"command": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		if h, ok := commandHandlers[update.Message.Command()]; ok {
			h(ctx, app, tgBot, update)
		}
	},
	"voice": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleVoiceMessage(ctx, tgBot, update)
	},
	"location": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleLocationMessage(ctx, tgBot, update)
	},
	"text": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleTextMessage(ctx, tgBot, update)
	},
	"callback": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleCallback(ctx, tgBot, update)
	},
}

func handleUpdate(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	updateType, _ := classifyUpdate(update)
	if h, ok := updateHandlers[updateType]; ok {
		h(ctx, app, tgBot, update)
	}
}

func runPolling(ctx context.Context, tgBot *tgbotapi.BotAPI, app *tghandlers.App, wg *sync.WaitGroup, log *zap.Logger) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := tgBot.GetUpdatesChan(u)

	sem := semaphore.NewWeighted(maxConcurrentUpdates)

	log.Info("bot started (polling mode)")

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down bot")
			tgBot.StopReceivingUpdates()
			wg.Wait()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := sem.Acquire(ctx, 1); err != nil {
				return
			}
			go func() {
				defer sem.Release(1)
				handleUpdateTraced(ctx, app, tgBot, &update)
			}()
		}
	}
}

func runWebhook(ctx context.Context, cfg *config.Config, tgBot *tgbotapi.BotAPI, app *tghandlers.App, wg *sync.WaitGroup, log *zap.Logger) {
	parsedURL, err := url.Parse(cfg.WebhookURL)
	if err != nil {
		log.Fatal("invalid WEBHOOK_URL", zap.Error(err))
	}

	wh := tgbotapi.WebhookConfig{URL: parsedURL}
	if _, err := tgBot.Request(wh); err != nil {
		log.Fatal("failed to set webhook", zap.Error(err))
	}
	log.Info("webhook registered", zap.String("url", cfg.WebhookURL))

	path := parsedURL.Path
	if path == "" {
		path = "/"
	}
	updates := tgBot.ListenForWebhook(path)

	srv := &http.Server{Addr: cfg.WebhookListenAddr}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("webhook server error", zap.Error(err))
		}
	}()
	log.Info("bot started (webhook mode)", zap.String("addr", cfg.WebhookListenAddr))

	sem := semaphore.NewWeighted(maxConcurrentUpdates)

	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down bot")

			if _, err := tgBot.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: false}); err != nil {
				log.Warn("failed to delete webhook", zap.Error(err))
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				log.Warn("webhook server shutdown error", zap.Error(err))
			}

			wg.Wait()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := sem.Acquire(ctx, 1); err != nil {
				return
			}
			go func() {
				defer sem.Release(1)
				handleUpdateTraced(ctx, app, tgBot, &update)
			}()
		}
	}
}
