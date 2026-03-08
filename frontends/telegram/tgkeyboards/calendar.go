package tgkeyboards

import (
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var monthNames = map[int]string{
	1: "Январь", 2: "Февраль", 3: "Март", 4: "Апрель",
	5: "Май", 6: "Июнь", 7: "Июль", 8: "Август",
	9: "Сентябрь", 10: "Октябрь", 11: "Ноябрь", 12: "Декабрь",
}

// MonthName returns the Russian month name for the given month number.
func MonthName(month int) string {
	return monthNames[month]
}

// Calendar builds the main calendar keyboard for date selection.
// existingDates is a set of dates in DD-MMM-YYYY format that have notes.
func Calendar(year, month int, activeDate string, existingDates map[string]bool) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	// Header row: prev / month+year / next
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀", "cal:prev"),
		tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("◀ %s %d ▶", monthNames[month], year), "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("▶", "cal:next"),
	))

	// Weekday headers
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Пн", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вт", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Ср", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Чт", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Пт", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Сб", "cal:noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вс", "cal:noop"),
	))

	// Day cells
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	// Monday-based weekday offset
	startOffset := int(firstDay.Weekday()+6) % 7
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()

	day := 1
	for row := 0; row < 6 && day <= daysInMonth; row++ {
		var weekRow []tgbotapi.InlineKeyboardButton
		for col := 0; col < 7; col++ {
			if (row == 0 && col < startOffset) || day > daysInMonth {
				weekRow = append(weekRow, tgbotapi.NewInlineKeyboardButtonData(" ", "cal:noop"))
			} else {
				dateStr := fmt.Sprintf("%02d-%s-%d", day, time.Month(month).String()[:3], year)
				label := fmt.Sprintf("%d", day)
				if dateStr == activeDate {
					label = fmt.Sprintf("[%d]", day)
				} else if existingDates[dateStr] {
					label = fmt.Sprintf("*%d*", day)
				}
				weekRow = append(weekRow, tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cal:select:%s", dateStr)))
				day++
			}
		}
		rows = append(rows, weekRow)
		if day > daysInMonth {
			break
		}
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📅 Сегодня", "cal:today"),
		tgbotapi.NewInlineKeyboardButtonData("◀ Назад", "cal:back"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
