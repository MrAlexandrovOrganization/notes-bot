package tghandlers

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/tgkeyboards"
)

func (a *App) HandleStart(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	userID := update.Message.From.ID
	if a.Cfg.RootID != 0 && userID != a.Cfg.RootID {
		sendText(tgBot, update.Message.Chat.ID, "⛔ Unauthorized access\\.", nil)
		a.Logger.Warn("unauthorized access attempt", zap.Int64("user_id", userID))
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		a.Logger.Error("get context", zap.Error(err))
		return
	}

	text := fmt.Sprintf(
		"👋 Добро пожаловать\\!\n\n📅 Активная дата: %s\n\nВыберите действие:",
		bot.EscapeMarkdownV2(uc.ActiveDate),
	)
	kb := tgkeyboards.MainMenu(uc.ActiveDate)
	if err := replyToUpdate(tgBot, update, text, &kb); err != nil {
		a.Logger.Error("send start message", zap.Error(err))
	}
	a.Logger.Info("user started bot", zap.Int64("user_id", userID))
}
