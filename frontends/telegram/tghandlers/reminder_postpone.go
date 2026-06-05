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
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

// minutesToLabel converts a minute count to a human-readable Russian label.
// Mixed durations are rendered as "1 д. 3 ч.", "2 ч. 30 мин.", etc.
func minutesToLabel(n int) string {
	const month = 30 * 24 * 60
	const week = 7 * 24 * 60
	const day = 24 * 60
	const hour = 60

	var parts []string
	if n >= month {
		parts = append(parts, fmt.Sprintf("%d мес.", n/month))
		n %= month
	}
	if n >= week {
		parts = append(parts, fmt.Sprintf("%d нед.", n/week))
		n %= week
	}
	if n >= day {
		parts = append(parts, fmt.Sprintf("%d д.", n/day))
		n %= day
	}
	if n >= hour {
		parts = append(parts, fmt.Sprintf("%d ч.", n/hour))
		n %= hour
	}
	if n > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d мин.", n))
	}
	return strings.Join(parts, " ")
}

// parseDuration parses a human-readable duration string into total minutes.
//
// Supported units (case-sensitive):
//
//	m — минуты   h — часы   d — дни   w — недели   M — месяцы (≈ 30 дней)
//
// Formats:
//
//	30m  2h30m  1d12h  1w  1M  3d6h30m  (spaces between tokens are OK)
//
// A bare integer is accepted as minutes for backward compatibility.
//
// Returns an informative error with a suggested canonical form when a unit
// value overflows into the next unit (e.g. 27h → error suggesting "1d3h").
func parseDuration(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Bare integer → minutes (backward compat)
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("введите положительное значение")
		}
		return n, nil
	}

	// Remove spaces so "1d 3h 30m" → "1d3h30m"
	s = strings.ReplaceAll(s, " ", "")

	vals := make(map[byte]int)
	i := 0
	for i < len(s) {
		// Read run of digits
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return 0, fmt.Errorf("неверный формат — ожидается число перед единицей. Примеры: 30m, 2h30m, 1d12h")
		}
		if j >= len(s) {
			return 0, fmt.Errorf("неверный формат — укажите единицу после числа. Доступные: m h d w M")
		}
		n, _ := strconv.Atoi(s[i:j])
		unit := s[j]
		switch unit {
		case 'm', 'h', 'd', 'w', 'M':
		default:
			return 0, fmt.Errorf("неизвестная единица %q. Доступные: m (минуты), h (часы), d (дни), w (недели), M (месяцы)", string(unit))
		}
		if _, exists := vals[unit]; exists {
			return 0, fmt.Errorf("единица %q указана дважды", string(unit))
		}
		vals[unit] = n
		i = j + 1
	}

	if len(vals) == 0 {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Validate: each unit must be within its canonical range.
	if m, ok := vals['m']; ok && m >= 60 {
		sugg := durationSuggestion(vals, 'm')
		return 0, fmt.Errorf("%dm — это %s; введите: %s", m, durationOverflowDesc(m, 'm'), sugg)
	}
	if h, ok := vals['h']; ok && h >= 24 {
		sugg := durationSuggestion(vals, 'h')
		return 0, fmt.Errorf("%dh — это %s; введите: %s", h, durationOverflowDesc(h, 'h'), sugg)
	}
	if d, ok := vals['d']; ok && d >= 7 {
		sugg := durationSuggestion(vals, 'd')
		return 0, fmt.Errorf("%dd — это %s; введите: %s", d, durationOverflowDesc(d, 'd'), sugg)
	}

	total := vals['m'] + vals['h']*60 + vals['d']*1440 + vals['w']*10080 + vals['M']*43200
	if total <= 0 {
		return 0, fmt.Errorf("введите положительное значение")
	}
	return total, nil
}

// durationOverflowDesc returns a human-readable description of what an
// overflowing unit value actually equals. E.g. 27 hours → "1д 3ч".
func durationOverflowDesc(val int, unit byte) string {
	switch unit {
	case 'm':
		h, m := val/60, val%60
		if m > 0 {
			return fmt.Sprintf("%dч %dм", h, m)
		}
		return fmt.Sprintf("%dч", h)
	case 'h':
		d, h := val/24, val%24
		if h > 0 {
			return fmt.Sprintf("%dд %dч", d, h)
		}
		return fmt.Sprintf("%dд", d)
	case 'd':
		w, d := val/7, val%7
		if d > 0 {
			return fmt.Sprintf("%dн %dд", w, d)
		}
		return fmt.Sprintf("%dн", w)
	}
	return fmt.Sprintf("%d", val)
}

// durationSuggestion builds a canonical duration string by normalising the
// overflowing unit and carrying into higher units.
func durationSuggestion(vals map[byte]int, overflowUnit byte) string {
	nv := make(map[byte]int, len(vals))
	for k, v := range vals {
		nv[k] = v
	}

	switch overflowUnit {
	case 'm':
		nv['h'] += nv['m'] / 60
		nv['m'] = nv['m'] % 60
		fallthrough // h might now overflow too
	case 'h':
		if overflowUnit == 'h' || nv['h'] >= 24 {
			nv['d'] += nv['h'] / 24
			nv['h'] = nv['h'] % 24
		}
		fallthrough
	case 'd':
		if overflowUnit == 'd' || nv['d'] >= 7 {
			nv['w'] += nv['d'] / 7
			nv['d'] = nv['d'] % 7
		}
	}

	var parts []string
	for _, u := range []byte{'M', 'w', 'd', 'h', 'm'} {
		if v := nv[u]; v > 0 {
			parts = append(parts, fmt.Sprintf("%d%c", v, u))
		}
	}
	if len(parts) == 0 {
		return "0m"
	}
	return strings.Join(parts, "")
}

// postponeWithMinutes calls PostponeReminder and shows the result as a callback edit.
func (a *App) postponeWithMinutes(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID, reminderID int64, minutes int32, label string) {
	log := applog.With(ctx, a.Logger)
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, minutes)
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}
	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape(fmt.Sprintf("%s\n\n⏰ Перенесено на %s.", original, label)+nextFireText), &kb)
	log.Info("reminder postponed", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

// HandleReminderPostponeInput handles "⏰ Перенести" — asks user to enter a duration.
func (a *App) HandleReminderPostponeInput(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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

	n, parseErr := parseDuration(text)
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

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape(fmt.Sprintf("⏰ Перенесено на %s.", minutesToLabel(n))+nextFireText), &kb)
	log.Info("reminder postponed via text", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID), zap.Int("minutes", n))
}

// HandleReminderPostponeDate handles "📅 На дату" — opens calendar for date selection.
func (a *App) HandleReminderPostponeDate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
