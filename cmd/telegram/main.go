package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/clients"
	"notes_bot/frontends/telegram/config"
	"notes_bot/frontends/telegram/tghandlers"
	"notes_bot/frontends/telegram/tgstates"
	"notes_bot/internal/telemetry"
)

var logger *zap.Logger

func init() {
	logger = zap.Must(zap.NewProduction())
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

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})
	defer rdb.Close()
	if err := redisotel.InstrumentTracing(rdb); err != nil {
		logger.Fatal("failed to instrument redis", zap.Error(err))
	}

	stateManager := tgstates.NewStateManager(rdb, cfg.TimezoneOffsetHours, cfg.DayStartHour)

	app := &tghandlers.App{
		Cfg:           cfg,
		Core:          coreClient,
		Notifications: notifClient,
		Whisper:       whisperClient,
		State:         stateManager,
		Logger:        logger,
	}

	// Telegram bot
	tgBot, err := tgbotapi.NewBotAPI(cfg.BOTToken)
	if err != nil {
		logger.Fatal("failed to create telegram bot", zap.Error(err))
	}
	logger.Info("bot authorized", zap.String("username", tgBot.Self.UserName))

	// Start Kafka consumer in background
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
	handleUpdate(ctx, app, tgBot, update)
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

func handleUpdate(ctx context.Context, app *tghandlers.App, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	// Give each update handler a generous timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch {
	case update.Message != nil && update.Message.IsCommand():
		switch update.Message.Command() {
		case "start":
			app.HandleStart(ctx, tgBot, update)
		}

	case update.Message != nil && (update.Message.Voice != nil || update.Message.VideoNote != nil):
		app.HandleVoiceMessage(ctx, tgBot, update)

	case update.Message != nil && update.Message.Text != "":
		app.HandleTextMessage(ctx, tgBot, update)

	case update.CallbackQuery != nil:
		app.HandleCallback(ctx, tgBot, update)
	}
}
