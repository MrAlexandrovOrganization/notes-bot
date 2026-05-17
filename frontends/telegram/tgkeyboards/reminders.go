package tgkeyboards

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"notes-bot/frontends/telegram/clients"
)

const remindersPerPage = 5

// ReminderNotification builds the action keyboard attached to a fired reminder message.
func ReminderNotification(reminderID int64, createTask bool, todayDate string) tgbotapi.InlineKeyboardMarkup {
	doneCB := fmt.Sprintf("reminder:done:%d:0", reminderID)
	if createTask && todayDate != "" {
		doneCB = fmt.Sprintf("reminder:done:%d:1:%s", reminderID, todayDate)
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Принято", doneCB),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("+1 ч", fmt.Sprintf("reminder:postpone_hours:1:%d", reminderID)),
			tgbotapi.NewInlineKeyboardButtonData("+3 ч", fmt.Sprintf("reminder:postpone_hours:3:%d", reminderID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("+1 д", fmt.Sprintf("reminder:postpone:1:%d", reminderID)),
			tgbotapi.NewInlineKeyboardButtonData("+3 д", fmt.Sprintf("reminder:postpone:3:%d", reminderID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Выбрать дату", fmt.Sprintf("reminder:custom_date:%d", reminderID)),
		),
	)
}

func RemindersList(reminders []*clients.ReminderInfo, page int) tgbotapi.InlineKeyboardMarkup {
	total := len(reminders)
	totalPages := (total + remindersPerPage - 1) / remindersPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	page = max(0, min(page, totalPages-1))
	start := page * remindersPerPage
	end := start + remindersPerPage
	if end > total {
		end = total
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, r := range reminders[start:end] {
		label := r.Title
		runes := []rune(label)
		if len(runes) > 30 {
			label = string(runes[:30])
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔔 "+label, "reminder:noop"),
			tgbotapi.NewInlineKeyboardButtonData("🗑", fmt.Sprintf("reminder:delete:%d", r.ID)),
		))
	}

	if totalPages > 1 {
		var nav []tgbotapi.InlineKeyboardButton
		if page > 0 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀", fmt.Sprintf("reminder:page:%d", page-1)))
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "reminder:noop"))
		if page < totalPages-1 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("▶", fmt.Sprintf("reminder:page:%d", page+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Создать", "reminder:create"),
		tgbotapi.NewInlineKeyboardButtonData("✍️ Текстом", "reminder:create_nl"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀ Назад", "reminder:back"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// NLReminderConfirm shows after the LLM parses a natural-language reminder.
func NLReminderConfirm() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Создать", "reminder:nl_confirm"),
			tgbotapi.NewInlineKeyboardButtonData("✏️ Вручную", "reminder:create"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "reminder:cancel"),
		),
	)
}

func TaskConfirm() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, создавать задачу", "reminder:task_confirm:yes"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "reminder:task_confirm:no"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "reminder:cancel"),
		),
	)
}

func ScheduleType() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Каждый день", "reminder:type:daily"),
			tgbotapi.NewInlineKeyboardButtonData("По дням недели", "reminder:type:weekly"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Каждый месяц", "reminder:type:monthly"),
			tgbotapi.NewInlineKeyboardButtonData("Каждый год", "reminder:type:yearly"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Один раз", "reminder:type:once"),
			tgbotapi.NewInlineKeyboardButtonData("Каждые N дней", "reminder:type:custom_days"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "reminder:cancel"),
		),
	)
}

func ReminderCancel() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "reminder:cancel"),
		),
	)
}

// ReminderCalendar builds a calendar for picking a date in reminder flows.
// contextName: "once" | "yr" (yearly) | "pp" (postpone)
func ReminderCalendar(year, month int, contextName string, tzOffsetHours int) tgbotapi.InlineKeyboardMarkup {
	tz := time.FixedZone("local", tzOffsetHours*3600)
	now := time.Now().In(tz)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	var rows [][]tgbotapi.InlineKeyboardButton

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀", fmt.Sprintf("reminder:cal:prev:%s", contextName)),
		tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s %d", monthNames[month], year), "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("▶", fmt.Sprintf("reminder:cal:next:%s", contextName)),
	))

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Пн", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вт", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Ср", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Чт", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Пт", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Сб", "reminder:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вс", "reminder:noop"),
	))

	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, tz)
	startOffset := int(firstDay.Weekday()+6) % 7
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, tz).Day()

	day := 1
	for row := 0; row < 6 && day <= daysInMonth; row++ {
		var weekRow []tgbotapi.InlineKeyboardButton
		for col := 0; col < 7; col++ {
			if (row == 0 && col < startOffset) || day > daysInMonth {
				weekRow = append(weekRow, tgbotapi.NewInlineKeyboardButtonData(" ", "reminder:noop"))
			} else {
				cellDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, tz)
				if cellDate.Before(today) {
					weekRow = append(weekRow, tgbotapi.NewInlineKeyboardButtonData(" ", "reminder:noop"))
				} else {
					dateStr := cellDate.Format("2006-01-02")
					label := fmt.Sprintf("%d", day)
					if cellDate.Equal(today) {
						label = fmt.Sprintf("[%d]", day)
					}
					weekRow = append(weekRow, tgbotapi.NewInlineKeyboardButtonData(
						label,
						fmt.Sprintf("reminder:cal:select:%s:%s", dateStr, contextName),
					))
				}
				day++
			}
		}
		rows = append(rows, weekRow)
		if day > daysInMonth {
			break
		}
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📅 Сегодня", fmt.Sprintf("reminder:cal:today:%s", contextName)),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "reminder:cancel"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
