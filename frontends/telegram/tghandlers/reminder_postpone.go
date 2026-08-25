package tghandlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/duration"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

// HandleReminderPostponeInput handles "⏰ Перенести" — asks user to enter a duration.
func (a *App) HandleReminderPostponeInput(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderPostponeInput
		u.PendingPostponeReminderID = reminderID
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query,
		tgfmt.Join(
			tgfmt.Escape("⏰ На сколько перенести?\n\nПоддерживаемые единицы: "),
			tgfmt.Code(tgfmt.Escape("m")), tgfmt.Escape(" мин · "),
			tgfmt.Code(tgfmt.Escape("h")), tgfmt.Escape(" ч · "),
			tgfmt.Code(tgfmt.Escape("d")), tgfmt.Escape(" дни · "),
			tgfmt.Code(tgfmt.Escape("w")), tgfmt.Escape(" недели · "),
			tgfmt.Code(tgfmt.Escape("M")), tgfmt.Escape(" месяцы\n\nПримеры: "),
			tgfmt.Code(tgfmt.Escape("30m")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("2h30m")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1d12h")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1w")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1M")),
			tgfmt.Escape("\nИли просто число минут: "),
			tgfmt.Code(tgfmt.Escape("90")),
			tgfmt.Escape(":"),
		),
		&kb)
}

// handleReminderPostponeTextInput parses a duration string and postpones the reminder.
// Accepts formats like 30m, 2h30m, 1d12h, 1w, 1M, or a plain integer (minutes).
func (a *App) handleReminderPostponeTextInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	cancelKb := tgkeyboards.ReminderCancel()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	reminderID := uc.PendingPostponeReminderID

	n, parseErr := duration.Parse(text)
	if parseErr != nil {
		replyToUpdate(ctx, tgBot, update,
			tgfmt.Join(tgfmt.Escape("❌ "+parseErr.Error())),
			&cancelKb)
		return
	}

	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, int32(n))
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.PendingPostponeReminderID = 0
	})

	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape(fmt.Sprintf("⏰ Перенесено на %s.", duration.MinutesToLabel(n))+nextFireText), &kb)
	log.Info("reminder postponed via text", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID), zap.Int("minutes", n))
}

// HandleReminderPostponeDate handles "📅 На дату" — opens calendar for date selection.
func (a *App) HandleReminderPostponeDate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderPostponeDate
		u.PendingPostponeReminderID = reminderID
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, "pp", a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("📅 Выберите дату переноса:"), &kb)
}

// handleReminderPostponeTimeInput parses HH:MM, computes minutes to the pending date+time,
// and calls PostponeReminder.
func (a *App) handleReminderPostponeTimeInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	cancelKb := tgkeyboards.ReminderCancel()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}

	parts := strings.SplitN(strings.TrimSpace(text), ":", 2)
	if len(parts) != 2 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
		return
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
		return
	}

	loc := time.FixedZone("tz", a.Cfg.TimezoneOffsetHours*3600)
	d, err := time.ParseInLocation("2006-01-02", uc.PendingPostponeDate, loc)
	if err != nil {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Дата потеряна. Выберите дату заново."), nil)
		return
	}
	target := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
	minutesUntil := int32(time.Until(target).Minutes())
	if minutesUntil < 1 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Выбранное время уже прошло. Введите другое время:"), &cancelKb)
		return
	}

	reminderID := uc.PendingPostponeReminderID
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, minutesUntil)
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.PendingPostponeReminderID = 0
		u.PendingPostponeDate = ""
	})

	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	label := fmt.Sprintf("%s %02d:%02d", uc.PendingPostponeDate, h, m)
	kb := a.getMainMenuKeyboard(ctx)
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape(fmt.Sprintf("⏰ Перенесено на %s.", label)+nextFireText), &kb)
	log.Info("reminder postponed to date+time", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}
