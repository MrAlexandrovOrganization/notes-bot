package tghandlers

import (
	"context"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

// HandleLocationMessage handles both one-time and live location messages.
// For initial location shares it saves and confirms to the user.
// For live location updates (EditedMessage) it saves silently.
func (a *App) HandleLocationMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	var msg *tgbotapi.Message
	isLiveUpdate := false

	switch {
	case update.Message != nil && update.Message.Location != nil:
		msg = update.Message
	case update.EditedMessage != nil && update.EditedMessage.Location != nil:
		msg = update.EditedMessage
		isLiveUpdate = true
	default:
		return
	}

	if msg.From == nil {
		return
	}
	userID := msg.From.ID
	if !a.authorized(userID) {
		return
	}

	loc := msg.Location
	span.SetAttributes(
		attribute.Float64("location.lat", loc.Latitude),
		attribute.Float64("location.lon", loc.Longitude),
		attribute.Bool("location.live_update", isLiveUpdate),
	)

	log := applog.With(ctx, a.Logger)

	// Get active date from user state; fall back to logical today.
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get user context", zap.Error(err))
	}
	activeDate := ""
	if uc != nil {
		activeDate = uc.ActiveDate
	}
	if activeDate == "" {
		activeDate = timeutil.TodayDate(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour)
	}

	input := &clients.SaveLocationInput{
		Latitude:   loc.Latitude,
		Longitude:  loc.Longitude,
		Accuracy:   float32(loc.HorizontalAccuracy),
		LivePeriod: loc.LivePeriod,
		Date:       activeDate,
		RecordedAt: time.Now(),
	}

	if _, err := a.Location.Save(ctx, input); err != nil {
		log.Error("save location", zap.Error(err))
		if !isLiveUpdate {
			chatID := msg.Chat.ID
			sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Ошибка при сохранении местоположения."), nil, true)
		}
		return
	}

	if isLiveUpdate {
		log.Info("live location saved",
			zap.Float64("lat", loc.Latitude),
			zap.Float64("lon", loc.Longitude),
			zap.String("date", activeDate),
		)
		return
	}

	chatID := msg.Chat.ID
	var text tgfmt.HTML
	if loc.LivePeriod > 0 {
		text = tgfmt.Escape(fmt.Sprintf(
			"📍 Живое местоположение активно — буду записывать каждое обновление к дате %s",
			activeDate,
		))
	} else {
		text = tgfmt.Escape(fmt.Sprintf(
			"📍 Местоположение сохранено к дате %s",
			activeDate,
		))
	}

	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, text, &kb, true)
}
