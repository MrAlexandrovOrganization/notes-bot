package tghandlers

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/bot"
	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

// confidenceThreshold — ниже этого порога подменяем intent на unknown
// и предлагаем пользователю выбрать вручную.
const confidenceThreshold = 0.6

// HandleSmartStart — entrypoint callback "menu:smart".
// Переводит юзера в StateSmartInput и просит описать действие.
func (a *App) HandleSmartStart(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateSmartInput
		u.SmartDraft = tgstates.SmartDraft{}
	})
	replyToCallback(ctx, tgBot, query,
		tgfmt.Escape("✨ Опиши, что хочешь сделать — текстом или голосом.\n\nПримеры:\n• Купил молоко\n• Позвонить маме\n• Завтра в 9 утра планёрка"),
		nil)
}

// handleSmartInput вызывает LLM-классификатор, складывает гипотезу в SmartDraft
// и показывает превью с подтверждением.
func (a *App) handleSmartInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)

	currentDateTime, today, tomorrow, dayAfter := a.llmDateContext()

	processingMsg, _ := tgBot.Send(tgbotapi.NewMessage(chatID, "🧠 Думаю..."))

	result, err := a.LLM.ClassifyIntent(ctx, text, currentDateTime)
	if err != nil {
		log.Error("LLM classify intent", zap.Error(err))
		editText(ctx, tgBot, chatID, processingMsg.MessageID,
			tgfmt.Escape("❌ Не удалось обработать запрос. Попробуй ещё раз или используй меню."),
			nil)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateIdle
		})
		return
	}

	intent := result.Intent
	if result.Confidence < confidenceThreshold {
		intent = clients.IntentUnknown
	}

	// Для reminder параметры берутся вторым вызовом — отдельные короткие schema
	// надёжнее одного большого JSON с вложенной структурой.
	var reminder *clients.LLMReminderResult
	if intent == clients.IntentReminder {
		r, err := a.LLM.ParseReminder(ctx, text, currentDateTime, today, tomorrow, dayAfter)
		if err != nil {
			log.Warn("smart: parse reminder failed, falling back to unknown", zap.Error(err))
			intent = clients.IntentUnknown
		} else {
			reminder = r
		}
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateSmartConfirm
		u.SmartDraft = tgstates.SmartDraft{
			RawText:    text,
			Intent:     intent,
			Confidence: result.Confidence,
			TaskTitle:  result.Title,
		}
		if intent == clients.IntentReminder && reminder != nil {
			u.ReminderDraft = tgstates.ReminderDraft{
				Title:        reminder.Title,
				ScheduleType: reminder.ScheduleType,
				Hour:         reminder.Hour,
				Minute:       reminder.Minute,
				Days:         reminder.Days,
				DayOfMonth:   reminder.DayOfMonth,
				Month:        reminder.Month,
				Day:          reminder.Day,
				Date:         reminder.Date,
				IntervalDays: reminder.IntervalDays,
				CreateTask:   reminder.CreateTask,
			}
		}
	})

	preview, kb := a.buildSmartPreview(intent, text, result, reminder)
	editText(ctx, tgBot, chatID, processingMsg.MessageID, preview, &kb)
	bot.SmartIntentTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("intent", intent)))
	log.Info("smart intent classified",
		zap.Int64("user_id", userID),
		zap.String("intent", intent),
		zap.Float64("confidence", result.Confidence),
	)
}

// buildSmartPreview формирует текст превью и клавиатуру в зависимости от intent.
func (a *App) buildSmartPreview(intent, rawText string, result *clients.LLMIntentResult, reminder *clients.LLMReminderResult) (tgfmt.HTML, tgbotapi.InlineKeyboardMarkup) {
	switch intent {
	case clients.IntentNote:
		return tgfmt.Escape(fmt.Sprintf("📝 Записать в заметку:\n\n«%s»", rawText)), tgkeyboards.SmartConfirm()

	case clients.IntentTask:
		title := result.Title
		if title == "" {
			title = rawText
		}
		return tgfmt.Escape(fmt.Sprintf("✅ Добавить задачу:\n\n«%s»", title)), tgkeyboards.SmartConfirm()

	case clients.IntentReminder:
		if reminder == nil {
			return tgfmt.Escape("⏰ Создать напоминание (детали не распознаны)."), tgkeyboards.SmartConfirm()
		}
		header := tgfmt.Escape("⏰ Создать напоминание:\n\n")
		body := formatNLReminderPreview(reminder)
		return tgfmt.Join(header, body), tgkeyboards.SmartConfirm()

	default:
		return tgfmt.Escape("🤔 Не поняла, что сделать. Выбери действие:"), tgkeyboards.SmartIntentPicker()
	}
}

// HandleSmartConfirm — callback "smart:yes": исполняем гипотезу из драфта.
func (a *App) HandleSmartConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	a.executeSmart(ctx, tgBot, query, userID, uc.SmartDraft.Intent, uc)
}

// HandleSmartReject — callback "smart:no": сбрасываем драфт и возвращаемся в idle.
func (a *App) HandleSmartReject(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, _ := a.State.GetContext(ctx, userID)
	intent := clients.IntentUnknown
	if uc != nil {
		intent = uc.SmartDraft.Intent
	}
	bot.SmartIntentRejected.Add(ctx, 1, metric.WithAttributes(attribute.String("intent", intent)))

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.SmartDraft = tgstates.SmartDraft{}
	})
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("Отменено."), &kb)
}

// HandleSmartPickIntent — callback "smart:pick:<intent>": пользователь сам
// выбрал, что делать с текстом (после unknown / low confidence).
func (a *App) HandleSmartPickIntent(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, intent string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}

	// Для reminder без распарсенных параметров не можем сразу исполнить —
	// отправляем raw text через старый NL-пайплайн.
	if intent == clients.IntentReminder && uc.ReminderDraft.ScheduleType == "" {
		// Переиспользуем существующий handleReminderNLInput.
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderCreateNL
		})
		a.handleReminderNLInput(ctx, tgBot, query.Message.Chat.ID, userID, uc.SmartDraft.RawText)
		return
	}

	a.executeSmart(ctx, tgBot, query, userID, intent, uc)
}

// executeSmart выполняет действие, соответствующее intent, и сбрасывает state в idle.
func (a *App) executeSmart(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, intent string, uc *tgstates.UserContext) {
	log := applog.With(ctx, a.Logger)
	rawText := uc.SmartDraft.RawText
	confirmedAttr := metric.WithAttributes(attribute.String("intent", intent))

	switch intent {
	case clients.IntentNote:
		ok, err := a.Core.AppendToNote(ctx, uc.ActiveDate, rawText)
		if err != nil || !ok {
			log.Error("smart: append to note", zap.Error(err))
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Не удалось добавить в заметку."), nil)
			return
		}
		a.resetSmart(ctx, userID)
		bot.SmartIntentConfirmed.Add(ctx, 1, confirmedAttr)
		kb := a.getMainMenuKeyboard(ctx)
		replyToCallback(ctx, tgBot, query,
			tgfmt.Join(
				tgfmt.Escape("✅ Текст добавлен в заметку "),
				tgfmt.Code(tgfmt.Escape(uc.ActiveDate)),
			), &kb)
		log.Info("smart: note saved", zap.Int64("user_id", userID))

	case clients.IntentTask:
		title := uc.SmartDraft.TaskTitle
		if title == "" {
			title = rawText
		}
		ok, err := a.Core.AddTask(ctx, uc.ActiveDate, title)
		if err != nil || !ok {
			log.Error("smart: add task", zap.Error(err))
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Не удалось добавить задачу."), nil)
			return
		}
		a.resetSmart(ctx, userID)
		bot.SmartIntentConfirmed.Add(ctx, 1, confirmedAttr)
		kb := a.getMainMenuKeyboard(ctx)
		replyToCallback(ctx, tgBot, query,
			tgfmt.Join(
				tgfmt.Escape("✅ Задача добавлена: "),
				tgfmt.Blockquote(tgfmt.Escape(title)),
			), &kb)
		log.Info("smart: task saved", zap.Int64("user_id", userID))

	case clients.IntentReminder:
		fakeUpdate := &tgbotapi.Update{Message: query.Message}
		a.finalizeReminderFromUpdate(ctx, tgBot, fakeUpdate, userID)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.SmartDraft = tgstates.SmartDraft{}
		})
		bot.SmartIntentConfirmed.Add(ctx, 1, confirmedAttr)

	default:
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Не поняла, что сделать."), nil)
	}
}

// resetSmart возвращает state в idle и очищает SmartDraft.
func (a *App) resetSmart(ctx context.Context, userID int64) {
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.SmartDraft = tgstates.SmartDraft{}
	})
}
