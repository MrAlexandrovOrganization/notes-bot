package tghandlers

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/bot"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

const locationHistoryLimit = 20

func (a *App) HandleMenuLocation(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	isActive, err := a.Notifications.GetLocationTrackingStatus(ctx, userID)
	if err != nil {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается."), nil)
	}

	a.State.UpdateContext(ctx, userID, func(uc *tgstates.UserContext) {
		uc.State = tgstates.StateLocationTracking
		uc.LocationTrackingActive = isActive
	})

	kb := tgkeyboards.LocationTrackingMenu(isActive)
	return replyToCallback(ctx, tgBot, query, tgfmt.Escape(locationStatusText(isActive)), &kb)
}

func (a *App) handleLocationAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("location.action", parts[1]))
	defer span.End()

	log := applog.With(ctx, a.Logger)

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in location action", zap.Any("recover", r), zap.String("stack", string(debug.Stack())))
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Внутренняя ошибка."), nil)
		}
	}()

	switch parts[1] {
	case "noop":
		return nil

	case "toggle":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		newActive := !uc.LocationTrackingActive

		if err := a.handleLocationToggle(ctx, userID, newActive); err != nil {
			return replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается."), nil)
		}

		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.LocationTrackingActive = newActive
		})

		if newActive {
			bot.LocationTrackingActive.Record(ctx, 1)
		} else {
			bot.LocationTrackingActive.Record(ctx, 0)
		}

		kb := tgkeyboards.LocationTrackingMenu(newActive)
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape(locationStatusText(newActive)), &kb)

	case "history":
		return a.showLocationHistory(ctx, tgBot, query, userID, 0)

	case "page":
		page := 0
		if len(parts) >= 3 {
			page, _ = strconv.Atoi(parts[2])
		}
		return a.showLocationHistory(ctx, tgBot, query, userID, page)

	case "menu":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		kb := tgkeyboards.LocationTrackingMenu(uc.LocationTrackingActive)
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape(locationStatusText(uc.LocationTrackingActive)), &kb)
	}
	return nil
}

func (a *App) handleLocationToggle(ctx context.Context, userID int64, active bool) error {
	_, err := a.Notifications.ToggleLocationTracking(ctx, userID, active)
	return err
}

func (a *App) showLocationHistory(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, page int) error {
	offset := page * locationHistoryLimit
	locs, err := a.Notifications.GetLocationHistory(ctx, userID, locationHistoryLimit, offset)
	if err != nil {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается."), nil)
	}

	if len(locs) == 0 {
		kb := tgkeyboards.LocationHistoryPage(page, false)
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("📍 История пуста."), &kb)
	}

	var sb strings.Builder
	sb.WriteString("📍 Последние локации:\n\n")
	for _, loc := range locs {
		ts := loc.RecordedAt.In(time.Local).Format("02.01 15:04")
		coord := formatCoordShort(loc.Latitude, loc.Longitude)
		src := loc.Source
		if loc.LiveMessageID != 0 {
			src = "🔴 live"
		}
		sb.WriteString(fmt.Sprintf("%s  %s  [%s]\n", ts, coord, src))
	}

	hasMore := len(locs) == locationHistoryLimit
	kb := tgkeyboards.LocationHistoryPage(page, hasMore)

	text := sb.String()
	if len(text) > 3800 {
		text = text[:3800] + "\n..."
	}

	return replyToCallback(ctx, tgBot, query, tgfmt.Escape(text), &kb)
}

func locationStatusText(isActive bool) string {
	if isActive {
		return "📍 Геолокация включена — отправляйте боту геолокацию и он будет её записывать. Поделитесь локацией в Telegram (📎 → 📍) и выберите «Передавать геоданные постоянно»."
	}
	return "📍 Геолокация выключена — нажмите кнопку ниже, чтобы включить. После включения отправляйте боту геолокацию и он будет её записывать в историю и метрики."
}

func formatCoordShort(lat, lon float64) string {
	return fmt.Sprintf("%.5f, %.5f", lat, lon)
}

func formatCoord(lat, lon float64) string {
	return fmt.Sprintf("%.6f, %.6f", lat, lon)
}

func (a *App) HandleLocationMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.Location == nil || update.Message.From == nil {
		return
	}

	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	userID := update.Message.From.ID
	if !a.authorized(userID) {
		sendText(ctx, tgBot, update.Message.Chat.ID, tgfmt.Escape("⛔ Unauthorized access."), nil, true)
		log.Warn("unauthorized location", zap.Int64("user_id", userID))
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in location handler", zap.Any("recover", r), zap.String("stack", string(debug.Stack())))
		}
	}()

	loc := update.Message.Location
	isLive := loc.LivePeriod > 0
	source := "static"
	var liveMsgID int64
	if isLive {
		source = "live"
		liveMsgID = int64(update.Message.MessageID)
	}

	_, err = a.Notifications.StoreLocation(ctx, userID,
		loc.Latitude, loc.Longitude,
		loc.HorizontalAccuracy, 0, float64(loc.Heading), 0,
		source, liveMsgID,
	)
	if err != nil {
		log.Error("store location", zap.Error(err))
		return
	}

	span.SetAttributes(
		attribute.Float64("location.latitude", loc.Latitude),
		attribute.Float64("location.longitude", loc.Longitude),
		attribute.String("location.source", source),
	)
	bot.LocationUpdates.Add(ctx, 1,
		metric.WithAttributes(attribute.String("source", source)),
	)
	bot.LocationLatestLat.Record(ctx, loc.Latitude)
	bot.LocationLatestLon.Record(ctx, loc.Longitude)

	if !uc.LocationTrackingActive {
		return
	}

	text := tgfmt.Join(
		tgfmt.Escape("📍 Геолокация сохранена!"),
		tgfmt.Escape(formatCoord(loc.Latitude, loc.Longitude)),
	)
	sendText(ctx, tgBot, update.Message.Chat.ID, text, nil, true)
}
