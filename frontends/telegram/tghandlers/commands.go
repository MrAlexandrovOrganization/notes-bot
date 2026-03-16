package tghandlers

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes_bot/internal/telemetry"
)

func (a *App) HandleStart(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if update.Message == nil || update.Message.From == nil {
		return
	}

	userID := update.Message.From.ID
	if !a.authorized(userID) {
		sendText(ctx, tgBot, update.Message.Chat.ID, "⛔ Unauthorized access.", nil, true)
		a.Logger.Warn("unauthorized access attempt", zap.Int64("user_id", userID))
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		a.Logger.Error("get context", zap.Error(err))
		return
	}

	text := fmt.Sprintf(
		"👋 Добро пожаловать!\n\n📅 Активная дата: %s\n\nВыберите действие:",
		uc.ActiveDate,
	)
	kb := a.getMainMenuKeyboard(ctx, userID)
	if err := replyToUpdate(ctx, tgBot, update, text, &kb); err != nil {
		a.Logger.Error("send start message", zap.Error(err))
	}
	a.Logger.Info("user started bot", zap.Int64("user_id", userID))
}
