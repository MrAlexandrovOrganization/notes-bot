package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

// ── List & Navigation ──────────────────────────────────────────────────────

func (a *App) HandleMenuNotifications(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})

	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается. Попробуйте через несколько секунд."), nil)
			return
		}
		log.Error("list reminders", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке напоминаний."), nil)
		return
	}

	page := uc.ReminderListPage
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(ctx, tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
	log.Info("user opened reminders", zap.Int64("user_id", userID))
}

func (a *App) HandleReminderPage(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, page int) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderListPage = page
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке напоминаний."), nil)
		return
	}
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(ctx, tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
}

// ── Delete, Done, Reject, Back, Cancel ────────────────────────────────────

func (a *App) HandleReminderDelete(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	if _, err := a.Notifications.DeleteReminder(ctx, reminderID, userID); err != nil {
		log.Error("delete reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при удалении напоминания."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		reminders = nil // fallback to empty list
	}
	kb := tgkeyboards.RemindersList(reminders, 0)
	var text tgfmt.HTML
	if len(reminders) > 0 {
		text = tgfmt.Raw("Напоминание удалено.\n\n") + reminderListText(reminders, 0, a.Cfg.TimezoneOffsetHours)
	} else {
		text = tgfmt.Raw("Напоминание удалено.\n\n🔔 Уведомления:\n\nНапоминаний пока нет.")
	}
	replyToCallback(ctx, tgBot, query, text, &kb)
	log.Info("deleted reminder", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) getMainMenuKeyboard(ctx context.Context) tgbotapi.InlineKeyboardMarkup {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	return tgkeyboards.MainMenu("")
}

func (a *App) HandleReminderDone(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64, createTaskFlag int, dateStr string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	if createTaskFlag == 1 && dateStr != "" && query.Message != nil {
		msgText := query.Message.Text
		title := strings.TrimPrefix(msgText, "🔔 Напоминание: ")
		tasks, err := a.Core.GetTasks(ctx, dateStr)
		if err == nil {
			for _, t := range tasks {
				if t.Text == title && !t.Completed {
					a.Core.ToggleTask(ctx, dateStr, t.Index)
					break
				}
			}
		}
	}

	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}

	kb := a.getMainMenuKeyboard(ctx)

	replyToCallback(ctx, tgBot, query, tgfmt.Escape(original+"\n\n✅ Принято!"), &kb)
	log.Info("reminder acknowledged", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

// HandleReminderReject dismisses the current reminder firing without affecting the schedule.
func (a *App) HandleReminderReject(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape(original+"\n\n❌ Отклонено."), &kb) // TODO: display reminder as Block quote
	applog.With(ctx, a.Logger).Info("reminder rejected", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderBack(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		applog.With(ctx, a.Logger).Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.ReminderDraft = tgstates.ReminderDraft{}
		u.PendingPostponeReminderID = 0
	})
	text := tgfmt.Join(
		tgfmt.Escape("\n\n📅 Активная дата: "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
		tgfmt.Escape("\n\nВыберите действие:"),
	)
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) HandleReminderCancel(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		reminders = nil // fallback to empty list
	}
	kb := tgkeyboards.RemindersList(reminders, 0)
	var text tgfmt.HTML
	if len(reminders) == 0 {
		text = tgfmt.Raw("🔔 Уведомления:\n\nНапоминаний пока нет.")
	} else {
		text = tgfmt.Raw("🔔 Уведомления:")
	}
	replyToCallback(ctx, tgBot, query, text, &kb)
}
