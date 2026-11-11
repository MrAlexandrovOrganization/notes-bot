package tghandlers

import (
	"context"
	"encoding/json"

	"github.com/mymmrac/telego"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

// HandleLocationMessage handles both one-time and live location messages.
// For initial location shares it saves and confirms to the user.
// For live location updates (EditedMessage) it saves silently.
func (a *App) HandleLocationMessage(ctx context.Context, tgBot *telego.Bot, update *telego.Update) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	var msg *telego.Message
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
	uc, stateErr := a.State.GetContext(ctx, userID)
	if stateErr != nil {
		if !isLiveUpdate {
			sendText(ctx, tgBot, msg.Chat.ID, tgfmt.Escape(resources.LocationSaveError), nil, true)
		}
		return
	}
	if uc.State == tgstates.StateGeoPoint {
		if isLiveUpdate || loc.LivePeriod > 0 || !validGeoPoint(loc.Latitude, loc.Longitude) {
			a.replyToStateMessage(ctx, tgBot, msg.Chat.ID, userID, tgfmt.Escape(resources.GeoPointPrompt), tgkeyboards.GeoCancel())
			return
		}
		if err := a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.GeoDraft.Latitude = loc.Latitude
			u.GeoDraft.Longitude = loc.Longitude
			u.State = tgstates.StateGeoRadius
		}); err != nil {
			return
		}
		a.replyToStateMessage(ctx, tgBot, msg.Chat.ID, userID, tgfmt.Escape(resources.GeoRadiusPrompt), tgkeyboards.GeoCancel())
		return
	}
	span.SetAttributes(
		attribute.Float64("location.lat", loc.Latitude),
		attribute.Float64("location.lon", loc.Longitude),
		attribute.Bool("location.live_update", isLiveUpdate),
	)

	log := applog.With(ctx, a.Logger)

	raw, err := json.Marshal(struct {
		UpdateID            int              `json:"update_id"`
		ChatID              int64            `json:"chat_id"`
		MessageID           int              `json:"message_id"`
		MessageDate         int64            `json:"message_date"`
		EditDate            int64            `json:"edit_date"`
		Location            *telego.Location `json:"location"`
		TimezoneOffsetHours int              `json:"timezone_offset_hours"`
		DayStartHour        int              `json:"day_start_hour"`
	}{update.UpdateID, msg.Chat.ID, msg.MessageID, msg.Date, msg.EditDate, loc, a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour})
	if err != nil {
		log.Error("encode location measurement", zap.Error(err))
		return
	}
	saved, err := a.Location.Save(ctx, &clients.SaveLocationInput{TelegramRaw: raw})
	if err != nil {
		log.Error("save location", zap.Error(err))
		if !isLiveUpdate {
			sendText(ctx, tgBot, msg.Chat.ID, tgfmt.Escape(resources.LocationSaveError), nil, true)
		}
		return
	}
	if isLiveUpdate {
		return
	}
	chatID := msg.Chat.ID
	text := tgfmt.Escape(resources.LocationSaved(saved.Date, loc.LivePeriod > 0))

	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, text, &kb, true)
}
