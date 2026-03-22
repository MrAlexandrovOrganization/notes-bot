package tghandlers

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/tgstates"
	"notes_bot/internal/applog"
	"notes_bot/internal/telemetry"
)

type stateTextHandler func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, chatID, userID int64, text string, uc *tgstates.UserContext)

var stateTextHandlers = map[tgstates.UserState]stateTextHandler{
	tgstates.StateWaitingRating: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, _ *tgbotapi.Update, chatID, userID int64, text string, uc *tgstates.UserContext) {
		a.handleRatingInput(ctx, tgBot, chatID, userID, text, uc.ActiveDate)
	},
	tgstates.StateReminderCreateTitle: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, _, userID int64, text string, _ *tgstates.UserContext) {
		a.handleReminderTitleInput(ctx, tgBot, update, userID, text)
	},
	tgstates.StateReminderCreateTime: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, _, userID int64, text string, _ *tgstates.UserContext) {
		a.handleReminderParamInput(ctx, tgBot, update, userID, text)
	},
	tgstates.StateReminderCreateDay: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, _, userID int64, text string, _ *tgstates.UserContext) {
		a.handleReminderParamInput(ctx, tgBot, update, userID, text)
	},
	tgstates.StateReminderCreateInterval: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, _, userID int64, text string, _ *tgstates.UserContext) {
		a.handleReminderParamInput(ctx, tgBot, update, userID, text)
	},
	tgstates.StateReminderCreateDate: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, _, userID int64, text string, _ *tgstates.UserContext) {
		a.handleReminderParamInput(ctx, tgBot, update, userID, text)
	},
	tgstates.StateWaitingNewTask: func(a *App, ctx context.Context, tgBot *tgbotapi.BotAPI, _ *tgbotapi.Update, chatID, userID int64, text string, uc *tgstates.UserContext) {
		a.handleAddTaskInput(ctx, tgBot, chatID, userID, text, uc.ActiveDate)
	},
}

func (a *App) HandleTextMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.From == nil || update.Message.Text == "" {
		return
	}

	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	userID := update.Message.From.ID
	if !a.authorized(userID) {
		sendText(ctx, tgBot, update.Message.Chat.ID, "⛔ Unauthorized access.", nil, true)
		log.Warn("unauthorized message", zap.Int64("user_id", userID))
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}
	span.SetAttributes(attribute.String("user.state", string(uc.State)))

	chatID := update.Message.Chat.ID

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in text handler", zap.Any("recover", r), zap.String("stack", string(debug.Stack())))
		}
	}()

	if h, ok := stateTextHandlers[uc.State]; ok {
		h(a, ctx, tgBot, update, chatID, userID, text, uc)
	} else {
		a.handleAppendNote(ctx, tgBot, chatID, userID, text, uc.ActiveDate)
	}
}

func (a *App) handleRatingInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	ctx, span := telemetry.StartSpan(ctx, attribute.String("note.date", activeDate))
	defer span.End()

	log := applog.With(ctx, a.Logger)
	rating, err := strconv.Atoi(text)
	if err != nil || rating < 0 || rating > 10 {
		sendText(ctx, tgBot, chatID, "❌ Оценка должна быть от 0 до 10. Попробуйте снова.", nil, true)
		return
	}

	ok, err := a.Core.UpdateRating(ctx, activeDate, rating)
	if err != nil || !ok {
		sendText(ctx, tgBot, chatID, "❌ Ошибка при сохранении оценки.", nil, true)
		return
	}

	a.State.UpdateContext(ctx, userID, func(uc *tgstates.UserContext) {
		uc.State = tgstates.StateIdle
	})
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, fmt.Sprintf("✅ Оценка %d сохранена!", rating), &kb, true)
	log.Info("user set rating", zap.Int64("user_id", userID), zap.Int("rating", rating))
}

func (a *App) handleAddTaskInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	ctx, span := telemetry.StartSpan(ctx, attribute.String("note.date", activeDate))
	defer span.End()

	log := applog.With(ctx, a.Logger)
	ok, err := a.Core.AddTask(ctx, activeDate, text)
	if err != nil || !ok {
		sendText(ctx, tgBot, chatID, "❌ Ошибка при добавлении задачи.", nil, true)
		return
	}
	a.State.UpdateContext(ctx, userID, func(uc *tgstates.UserContext) {
		uc.State = tgstates.StateTasksView
	})
	sendText(ctx, tgBot, chatID, fmt.Sprintf("✅ Задача добавлена: %s", text), nil, true)
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, "Используйте кнопку \"Задачи\" для просмотра.", &kb, true)
	log.Info("user added task", zap.Int64("user_id", userID))
}

func (a *App) handleAppendNote(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text, activeDate string) {
	ctx, span := telemetry.StartSpan(ctx, attribute.String("note.date", activeDate))
	defer span.End()

	log := applog.With(ctx, a.Logger)
	ok, err := a.Core.AppendToNote(ctx, activeDate, text)
	if err != nil || !ok {
		sendText(ctx, tgBot, chatID, "❌ Ошибка при сохранении текста.", nil, true)
		return
	}
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, fmt.Sprintf("✅ Текст добавлен в заметку %s", activeDate), &kb, true)
	log.Info("user appended text", zap.Int64("user_id", userID))
}
