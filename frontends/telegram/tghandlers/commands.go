package tghandlers

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/internal/telemetry"
)

func (a *App) HandleStart(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if update.Message == nil || update.Message.From == nil {
		return
	}

	userID := update.Message.From.ID
	if !a.authorized(userID) {
		sendText(ctx, tgBot, update.Message.Chat.ID, tgfmt.Escape("⛔ Unauthorized access."), nil, true)
		a.Logger.Warn("unauthorized access attempt", zap.Int64("user_id", userID))
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		a.Logger.Error("get context", zap.Error(err))
		return
	}

	text := tgfmt.Join(
		tgfmt.Escape("👋 Добро пожаловать!\n\n📅 Активная дата: "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
		tgfmt.Escape("\n\nВыберите действие:"),
	)
	kb := a.getMainMenuKeyboard(ctx)
	if err := replyToUpdate(ctx, tgBot, update, text, &kb); err != nil {
		a.Logger.Error("send start message", zap.Error(err))
	}
	a.Logger.Info("user started bot", zap.Int64("user_id", userID))
}
