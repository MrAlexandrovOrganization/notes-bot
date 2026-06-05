package tghandlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

// formatNLReminderPreview builds a human-readable summary of the LLM-parsed reminder.
func formatNLReminderPreview(r *clients.LLMReminderResult) tgfmt.HTML {
	schedule := scheduleLabel(r.ScheduleType)
	switch r.ScheduleType {
	case "weekly":
		var daysStr []string
		for _, d := range r.Days {
			if d >= 0 && d <= 6 {
				daysStr = append(daysStr, dayNamesRu[d])
			}
		}
		if len(daysStr) > 0 {
			schedule = "по " + strings.Join(daysStr, ", ")
		}
	case "monthly":
		schedule = fmt.Sprintf("каждый месяц %d числа", r.DayOfMonth)
	case "yearly":
		schedule = fmt.Sprintf("каждый год %d.%02d", r.Day, r.Month)
	case "once":
		if t, err := time.Parse("2006-01-02", r.Date); err == nil {
			schedule = fmt.Sprintf("один раз, %d %s %d", t.Day(), monthNamesRu[int(t.Month())], t.Year())
		} else if r.Date != "" {
			schedule = fmt.Sprintf("один раз, %s", r.Date)
		} else {
			schedule = "один раз, ⚠️ дата не распознана"
		}
	case "custom_days":
		schedule = fmt.Sprintf("каждые %d %s", r.IntervalDays, pluralDays(r.IntervalDays))
	}

	taskNote := "нет"
	if r.CreateTask {
		taskNote = "да"
	}

	return tgfmt.Escape(fmt.Sprintf(
		"🧠 Я понял так:\n\n📌 Название: %s\n🔄 Расписание: %s\n⏰ Время: %02d:%02d\n📋 Создавать задачу: %s",
		r.Title, schedule, r.Hour, r.Minute, taskNote,
	))
}

func (a *App) HandleReminderCreateNL(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateNL
		u.ReminderDraft = tgstates.ReminderDraft{}
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query,
		tgfmt.Escape("✍️ Опишите напоминание одной фразой.\n\nПримеры:\n• каждый день в 9 утра пить воду\n• каждый понедельник в 9:00 планирование недели\n• 25 числа каждого месяца оплатить аренду\n\nМожно отправить голосовое сообщение."),
		&kb)
}

// handleReminderNLInput processes a natural-language reminder description (from text or voice).
func (a *App) handleReminderNLInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)

	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	currentDateTime := now.Format("2006-01-02 15:04")

	processingMsg, _ := tgBot.Send(tgbotapi.NewMessage(chatID, "🧠 Обрабатываю..."))

	result, err := a.LLM.ParseReminder(ctx, text, currentDateTime)
	if err != nil {
		log.Error("LLM parse reminder", zap.Error(err))
		cancelKb := tgkeyboards.ReminderCancel()
		editText(ctx, tgBot, chatID, processingMsg.MessageID,
			tgfmt.Escape("❌ Не удалось обработать запрос. Попробуйте создать напоминание вручную."),
			&cancelKb)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderList
		})
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderDraft = tgstates.ReminderDraft{
			Title:        result.Title,
			ScheduleType: result.ScheduleType,
			Hour:         result.Hour,
			Minute:       result.Minute,
			Days:         result.Days,
			DayOfMonth:   result.DayOfMonth,
			Month:        result.Month,
			Day:          result.Day,
			Date:         result.Date,
			IntervalDays: result.IntervalDays,
			CreateTask:   result.CreateTask,
		}
	})

	kb := tgkeyboards.NLReminderConfirm()
	editText(ctx, tgBot, chatID, processingMsg.MessageID, formatNLReminderPreview(result), &kb)
	log.Info("NL reminder parsed", zap.Int64("user_id", userID), zap.String("title", result.Title))
}

// HandleReminderNLConfirm finalizes a reminder that was parsed from natural language.
func (a *App) HandleReminderNLConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	// Build a fake Update so we can reuse finalizeReminderFromUpdate.
	fakeUpdate := &tgbotapi.Update{Message: query.Message}
	a.finalizeReminderFromUpdate(ctx, tgBot, fakeUpdate, userID)
}
