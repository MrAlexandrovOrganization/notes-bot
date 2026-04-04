package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/bot"
	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tghandlers"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

var logger *zap.Logger

func init() {
	logger = applog.New()
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
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
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler)
		logger.Info("starting metrics server", zap.String("port", metricsPort))
		if err := http.ListenAndServe(":"+metricsPort, mux); err != nil {
			logger.Error("metrics server stopped", zap.Error(err))
		}
	}()

	// Clients
	coreClient, err := clients.NewCoreClient(ctx, cfg.CoreGRPCHost, cfg.CoreGRPCPort)
	if err != nil {
		logger.Fatal("failed to create core client", zap.Error(err))
	}
	defer coreClient.Close()

	notifClient, err := clients.NewNotificationsClient(ctx, cfg.NotificationsGRPCHost, cfg.NotificationsGRPCPort)
	if err != nil {
		logger.Fatal("failed to create notifications client", zap.Error(err))
	}
	defer notifClient.Close()

	whisperClient, err := clients.NewWhisperClient(ctx, cfg.WhisperGRPCHost, cfg.WhisperGRPCPort)
	if err != nil {
		logger.Fatal("failed to create whisper client", zap.Error(err))
	}
	defer whisperClient.Close()

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

	app := &tghandlers.App{
		Cfg:           cfg,
		Core:          coreClient,
		Notifications: notifClient,
		Whisper:       whisperClient,
		LLM:           llmClient,
		State:         stateManager,
		Logger:        logger,
	}

	// Telegram bot
	tgBot, err := tgbotapi.NewBotAPI(cfg.BOTToken)
	if err != nil {
		logger.Fatal("failed to create telegram bot", zap.Error(err))
	}
	logger.Info("bot authorized", zap.String("username", tgBot.Self.UserName))

	// Start Kafka consumer in background.
	// Offsets are committed to Kafka via consumer group — no external store needed.
	go bot.RunKafkaConsumer(ctx, cfg.KafkaBootstrapServers, app.MakeReminderHandler(tgBot), logger)

	// Start polling
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := tgBot.GetUpdatesChan(u)

	logger.Info("bot started polling")

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down bot")
			tgBot.StopReceivingUpdates()
			return

		case update, ok := <-updates:
			if !ok {
				return
			}
			go handleUpdateTraced(ctx, app, tgBot, &update)
		}
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
	"text": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleTextMessage(ctx, tgBot, update)
	},
	"callback": func(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
		app.HandleCallback(ctx, tgBot, update)
	},
}

func handleUpdate(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	// Give each update handler a generous timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	updateType, _ := classifyUpdate(update)
	if h, ok := updateHandlers[updateType]; ok {
		h(ctx, app, tgBot, update)
	}
}
