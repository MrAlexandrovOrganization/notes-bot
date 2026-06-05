package tghandlers

import (
	"fmt"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/timeutil"
)

var scheduleLabels = map[string]string{
	"daily":       "каждый день",
	"weekly":      "по дням недели",
	"monthly":     "каждый месяц",
	"yearly":      "каждый год",
	"once":        "один раз",
	"custom_days": "каждые N дней",
}

func scheduleLabel(scheduleType string) string {
	if l, ok := scheduleLabels[scheduleType]; ok {
		return l
	}
	return scheduleType
}

func reminderListText(reminders []*clients.ReminderInfo, page, tzOffset int) tgfmt.HTML {
	header := tgfmt.Raw("🔔 Уведомления:\n\n")
	amountReminders := len(reminders)
	if amountReminders == 0 {
		return header + tgfmt.Raw("Напоминаний пока нет.")
	}
	perPage := 5
	page = min((amountReminders-1)/perPage, page)

	start := page * perPage
	end := min(start+perPage, amountReminders)
	lines := make([]tgfmt.HTML, 0, end-start)
	for _, r := range reminders[start:end] {
		lines = append(lines, tgfmt.Join(
			tgfmt.Escape("• "),
			tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", r.Title))),
			tgfmt.Escape(fmt.Sprintf(" (%s) — %s",
				scheduleLabel(r.ScheduleType),
				timeutil.FormatLocalTime(r.NextFireAt, tzOffset),
			))))
	}
	parts := make([]tgfmt.HTML, 0, len(lines)*2)
	for i, l := range lines {
		parts = append(parts, l)
		if i < len(lines)-1 {
			parts = append(parts, tgfmt.Raw("\n"))
		}
	}
	return header + tgfmt.Join(parts...)
}

func calMonthYear(uc *tgstates.UserContext, tzOffset int) (int, int) {
	now := timeutil.LocalNow(tzOffset)
	month := uc.ReminderCalMonth
	year := uc.ReminderCalYear
	if month == 0 {
		month = int(now.Month())
	}
	if year == 0 {
		year = now.Year()
	}
	return month, year
}

var dayNamesRu = []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

var monthNamesRu = map[int]string{
	1: "января", 2: "февраля", 3: "марта", 4: "апреля",
	5: "мая", 6: "июня", 7: "июля", 8: "августа",
	9: "сентября", 10: "октября", 11: "ноября", 12: "декабря",
}

func pluralDays(n int) string {
	mod10, mod100 := n%10, n%100
	switch {
	case mod10 == 1 && mod100 != 11:
		return "день"
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20):
		return "дня"
	default:
		return "дней"
	}
}
