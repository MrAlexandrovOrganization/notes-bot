package tgkeyboards

import (
	"fmt"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"notes-bot/frontends/telegram/clients"
)

const remindersPerPage = 5

// ReminderNotification builds the action keyboard attached to a fired reminder message.
func ReminderNotification(reminderID int64, createTask bool, todayDate string) telego.InlineKeyboardMarkup {
	doneCB := fmt.Sprintf("reminder:done:%d:0", reminderID)
	if createTask && todayDate != "" {
		doneCB = fmt.Sprintf("reminder:done:%d:1:%s", reminderID, todayDate)
	}
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✅ Принято").WithCallbackData(doneCB),
			tu.InlineKeyboardButton("❌ Отклонить").WithCallbackData(fmt.Sprintf("reminder:reject:%d", reminderID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("⏰ Перенести").WithCallbackData(fmt.Sprintf("reminder:postpone_input:%d", reminderID)),
			tu.InlineKeyboardButton("📅 На дату").WithCallbackData(fmt.Sprintf("reminder:postpone_date:%d", reminderID)),
		),
	)
}

func RemindersList(reminders []*clients.ReminderInfo, page int) telego.InlineKeyboardMarkup {
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

	var rows [][]telego.InlineKeyboardButton
	for _, r := range reminders[start:end] {
		label := r.Title
		runes := []rune(label)
		if len(runes) > 30 {
			label = string(runes[:30])
		}
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔔 "+label).WithCallbackData("reminder:noop"),
			tu.InlineKeyboardButton("🗑").WithCallbackData(fmt.Sprintf("reminder:delete:%d", r.ID)),
		))
	}

	if totalPages > 1 {
		var nav []telego.InlineKeyboardButton
		if page > 0 {
			nav = append(nav, tu.InlineKeyboardButton("◀").WithCallbackData(fmt.Sprintf("reminder:page:%d", page-1)))
		}
		nav = append(nav, tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", page+1, totalPages)).WithCallbackData("reminder:noop"))
		if page < totalPages-1 {
			nav = append(nav, tu.InlineKeyboardButton("▶").WithCallbackData(fmt.Sprintf("reminder:page:%d", page+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("➕ Создать").WithCallbackData("reminder:create"),
		tu.InlineKeyboardButton("✍️ Текстом").WithCallbackData("reminder:create_nl"),
	))
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀ Назад").WithCallbackData("reminder:back"),
	))

	return *tu.InlineKeyboard(rows...)
}

// NLReminderConfirm shows after the LLM parses a natural-language reminder.
func NLReminderConfirm() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✅ Создать").WithCallbackData("reminder:nl_confirm"),
			tu.InlineKeyboardButton("✏️ Вручную").WithCallbackData("reminder:create"),
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("reminder:cancel"),
		),
	)
}

func TaskConfirm() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✅ Да, создавать задачу").WithCallbackData("reminder:task_confirm:yes"),
			tu.InlineKeyboardButton("❌ Нет").WithCallbackData("reminder:task_confirm:no"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("reminder:cancel"),
		),
	)
}

func ScheduleType() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Каждый день").WithCallbackData("reminder:type:daily"),
			tu.InlineKeyboardButton("По дням недели").WithCallbackData("reminder:type:weekly"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Каждый месяц").WithCallbackData("reminder:type:monthly"),
			tu.InlineKeyboardButton("Каждый год").WithCallbackData("reminder:type:yearly"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Один раз").WithCallbackData("reminder:type:once"),
			tu.InlineKeyboardButton("Каждые N дней").WithCallbackData("reminder:type:custom_days"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("reminder:cancel"),
		),
	)
}

func ReminderCancel() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("reminder:cancel"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("◀ Назад").WithCallbackData("reminder:back"),
		),
	)
}

// ReminderCalendar builds a calendar for picking a date in reminder flows.
// contextName: "once" | "yr" (yearly) | "pp" (postpone)
func ReminderCalendar(year, month int, contextName string, tzOffsetHours int) telego.InlineKeyboardMarkup {
	tz := time.FixedZone("local", tzOffsetHours*3600)
	now := time.Now().In(tz)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, tz)

	var rows [][]telego.InlineKeyboardButton

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀").WithCallbackData(fmt.Sprintf("reminder:cal:prev:%s", contextName)),
		tu.InlineKeyboardButton(fmt.Sprintf("%s %d", monthNames[month], year)).WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("▶").WithCallbackData(fmt.Sprintf("reminder:cal:next:%s", contextName)),
	))

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("Пн").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Вт").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Ср").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Чт").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Пт").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Сб").WithCallbackData("reminder:noop"),
		tu.InlineKeyboardButton("Вс").WithCallbackData("reminder:noop"),
	))

	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, tz)
	startOffset := int(firstDay.Weekday()+6) % 7
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, tz).Day()

	day := 1
	for row := 0; row < 6 && day <= daysInMonth; row++ {
		var weekRow []telego.InlineKeyboardButton
		for col := 0; col < 7; col++ {
			if (row == 0 && col < startOffset) || day > daysInMonth {
				weekRow = append(weekRow, tu.InlineKeyboardButton(" ").WithCallbackData("reminder:noop"))
			} else {
				cellDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, tz)
				if cellDate.Before(today) {
					weekRow = append(weekRow, tu.InlineKeyboardButton(" ").WithCallbackData("reminder:noop"))
				} else {
					dateStr := cellDate.Format("2006-01-02")
					label := fmt.Sprintf("%d", day)
					if cellDate.Equal(today) {
						label = fmt.Sprintf("[%d]", day)
					}
					weekRow = append(weekRow, tu.InlineKeyboardButton(label).WithCallbackData(
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

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("📅 Сегодня").WithCallbackData(fmt.Sprintf("reminder:cal:today:%s", contextName)),
		tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("reminder:cancel"),
	))

	return *tu.InlineKeyboard(rows...)
}
