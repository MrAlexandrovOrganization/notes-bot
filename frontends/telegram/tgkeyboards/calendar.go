package tgkeyboards

import (
	"context"
	"fmt"
	"notes-bot/internal/telemetry"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
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
func Calendar(ctx context.Context, year, month int, activeDate string, existingDates map[string]bool) telego.InlineKeyboardMarkup {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	var rows [][]telego.InlineKeyboardButton

	// Header row: prev / month+year / next
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀").WithCallbackData("cal:prev"),
		tu.InlineKeyboardButton(fmt.Sprintf("◀ %s %d ▶", monthNames[month], year)).WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("▶").WithCallbackData("cal:next"),
	))

	// Weekday headers
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("Пн").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Вт").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Ср").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Чт").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Пт").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Сб").WithCallbackData("cal:noop"),
		tu.InlineKeyboardButton("Вс").WithCallbackData("cal:noop"),
	))

	// Day cells
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	// Monday-based weekday offset
	startOffset := int(firstDay.Weekday()+6) % 7
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()

	day := 1
	for row := 0; row < 6 && day <= daysInMonth; row++ {
		var weekRow []telego.InlineKeyboardButton
		for col := 0; col < 7; col++ {
			if (row == 0 && col < startOffset) || day > daysInMonth {
				weekRow = append(weekRow, tu.InlineKeyboardButton(" ").WithCallbackData("cal:noop"))
			} else {
				dateStr := fmt.Sprintf("%02d-%s-%d", day, time.Month(month).String()[:3], year)
				label := fmt.Sprintf("%d", day)
				if dateStr == activeDate {
					label = fmt.Sprintf("[%d]", day)
				} else if existingDates[dateStr] {
					label = fmt.Sprintf("*%d*", day)
				}
				weekRow = append(weekRow, tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("cal:select:%s", dateStr)))
				day++
			}
		}
		rows = append(rows, weekRow)
		if day > daysInMonth {
			break
		}
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("📅 Сегодня").WithCallbackData("cal:today"),
		tu.InlineKeyboardButton("◀ Назад").WithCallbackData("cal:back"),
	))

	return *tu.InlineKeyboard(rows...)
}
