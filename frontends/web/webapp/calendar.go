package webapp

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"notes-bot/frontends/web/views"
	"notes-bot/internal/timeutil"
)

func (a *App) registerCalendarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /calendar", a.handleCalendar)
}

func (a *App) handleCalendar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	year := queryInt(r, "year", now.Year())
	month := queryInt(r, "month", int(now.Month()))
	if month < 1 || month > 12 {
		month = int(now.Month())
	}

	existingDates, err := a.Core.GetExistingDates(ctx)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	existing := make(map[string]bool, len(existingDates))
	for _, d := range existingDates {
		existing[d] = true
	}

	today := timeutil.TodayDate(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour)

	a.render(w, r, views.Calendar(views.CalendarData{
		Year:      year,
		Month:     month,
		MonthName: monthNameRU(month),
		PrevYear:  prevMonthYear(year, month),
		PrevMonth: prevMonth(month),
		NextYear:  nextMonthYear(year, month),
		NextMonth: nextMonth(month),
		Today:     today,
		Weeks:     buildCalendarWeeks(year, month, today, existing),
	}))
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

var monthNamesRU = map[int]string{
	1: "Январь", 2: "Февраль", 3: "Март", 4: "Апрель",
	5: "Май", 6: "Июнь", 7: "Июль", 8: "Август",
	9: "Сентябрь", 10: "Октябрь", 11: "Ноябрь", 12: "Декабрь",
}

func monthNameRU(month int) string { return monthNamesRU[month] }

func prevMonth(month int) int {
	if month == 1 {
		return 12
	}
	return month - 1
}

func nextMonth(month int) int {
	if month == 12 {
		return 1
	}
	return month + 1
}

func prevMonthYear(year, month int) int {
	if month == 1 {
		return year - 1
	}
	return year
}

func nextMonthYear(year, month int) int {
	if month == 12 {
		return year + 1
	}
	return year
}

// buildCalendarWeeks lays out the month as Monday-start weeks, matching the
// convention used by the Telegram calendar keyboard (tgkeyboards/calendar.go).
func buildCalendarWeeks(year, month int, today string, existing map[string]bool) [][]views.CalendarDay {
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	startOffset := int(firstDay.Weekday()+6) % 7
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()

	var weeks [][]views.CalendarDay
	day := 1
	for row := 0; row < 6 && day <= daysInMonth; row++ {
		var week []views.CalendarDay
		for col := 0; col < 7; col++ {
			if (row == 0 && col < startOffset) || day > daysInMonth {
				week = append(week, views.CalendarDay{})
				continue
			}
			dateStr := fmt.Sprintf("%02d-%s-%d", day, time.Month(month).String()[:3], year)
			week = append(week, views.CalendarDay{
				Day:     day,
				Date:    dateStr,
				HasNote: existing[dateStr],
				IsToday: dateStr == today,
			})
			day++
		}
		weeks = append(weeks, week)
		if day > daysInMonth {
			break
		}
	}
	return weeks
}
