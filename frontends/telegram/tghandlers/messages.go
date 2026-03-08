package tghandlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/tgkeyboards"
	"notes_bot/frontends/telegram/tgstates"
)

func (a *App) HandleTextMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.From == nil || update.Message.Text == "" {
		return
	}

	userID := update.Message.From.ID
	if !a.authorized(userID) {
		sendText(tgBot, update.Message.Chat.ID, "⛔ Unauthorized access\\.", nil)
		a.Logger.Warn("unauthorized message", zap.Int64("user_id", userID))
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		a.Logger.Error("get context", zap.Error(err))
		return
	}

	chatID := update.Message.Chat.ID

	defer func() {
		if r := recover(); r != nil {
			a.Logger.Error("panic in text handler", zap.Any("recover", r))
		}
	}()

	switch uc.State {
	case tgstates.StateWaitingRating:
		a.handleRatingInput(ctx, tgBot, chatID, userID, text, uc.ActiveDate)

	case tgstates.StateReminderCreateTitle:
		a.handleReminderTitleInput(ctx, tgBot, update, userID, text)

	case tgstates.StateReminderCreateTime,
		tgstates.StateReminderCreateDay,
		tgstates.StateReminderCreateInterval,
		tgstates.StateReminderCreateDate:
		a.handleReminderParamInput(ctx, tgBot, update, userID, text)

	case tgstates.StateWaitingNewTask:
		a.handleAddTaskInput(ctx, tgBot, chatID, userID, text, uc.ActiveDate)

	default:
		a.handleAppendNote(ctx, tgBot, chatID, userID, text, uc.ActiveDate)
	}
}

func (a *App) handleRatingInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	rating, err := strconv.Atoi(text)
	if err != nil || rating < 0 || rating > 10 {
		sendText(tgBot, chatID, "❌ Оценка должна быть от 0 до 10\\. Попробуйте снова\\.", nil)
		return
	}

	ok, err := a.Core.UpdateRating(ctx, activeDate, rating)
	if err != nil || !ok {
		sendText(tgBot, chatID, "❌ Ошибка при сохранении оценки\\.", nil)
		return
	}

	a.State.UpdateContext(ctx, userID, func(uc *tgstates.UserContext) {
		uc.State = tgstates.StateIdle
	})
	kb := tgkeyboards.MainMenu(activeDate)
	sendText(tgBot, chatID, fmt.Sprintf("✅ Оценка %d сохранена\\!", rating), &kb)
	a.Logger.Info("user set rating", zap.Int64("user_id", userID), zap.Int("rating", rating))
}

func (a *App) handleAddTaskInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	ok, err := a.Core.AddTask(ctx, activeDate, text)
	if err != nil || !ok {
		sendText(tgBot, chatID, "❌ Ошибка при добавлении задачи\\.", nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(uc *tgstates.UserContext) {
		uc.State = tgstates.StateTasksView
	})
	sendText(tgBot, chatID, fmt.Sprintf("✅ Задача добавлена: %s", bot.EscapeMarkdownV2(text)), nil)
	kb := tgkeyboards.MainMenu(activeDate)
	sendText(tgBot, chatID, "Используйте кнопку \"Задачи\" для просмотра\\.", &kb)
	a.Logger.Info("user added task", zap.Int64("user_id", userID))
}

func (a *App) handleAppendNote(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	ok, err := a.Core.AppendToNote(ctx, activeDate, text)
	if err != nil || !ok {
		sendText(tgBot, chatID, "❌ Ошибка при сохранении текста\\.", nil)
		return
	}
	kb := tgkeyboards.MainMenu(activeDate)
	sendText(tgBot, chatID, fmt.Sprintf("✅ Текст добавлен в заметку %s", bot.EscapeMarkdownV2(activeDate)), &kb)
	a.Logger.Info("user appended text", zap.Int64("user_id", userID))
}
