package webapp

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/notifications"

	"notes-bot/frontends/web/views"
	"notes-bot/internal/timeutil"
)

func (a *App) registerReminderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /reminders", a.handleRemindersList)
	mux.HandleFunc("POST /reminders", a.handleCreateReminder)
	mux.HandleFunc("POST /reminders/nl", a.handleCreateReminderNL)
	mux.HandleFunc("POST /reminders/{id}/delete", a.handleDeleteReminder)
	mux.HandleFunc("POST /reminders/{id}/postpone", a.handlePostponeReminder)
}

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

func (a *App) handleRemindersList(w http.ResponseWriter, r *http.Request) {
	a.renderReminders(w, r, "", views.ReminderFormData{ScheduleType: "daily"})
}

func (a *App) renderReminders(w http.ResponseWriter, r *http.Request, formError string, form views.ReminderFormData) {
	reminders, err := a.Notifications.ListReminders(r.Context(), a.Cfg.RootID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]views.ReminderItem, len(reminders))
	for i, rem := range reminders {
		items[i] = views.ReminderItem{
			ID:           rem.ID,
			Title:        rem.Title,
			ScheduleType: scheduleLabel(rem.ScheduleType),
			NextFireAt:   timeutil.FormatLocalTime(rem.NextFireAt, a.Cfg.TimezoneOffsetHours),
		}
	}
	a.render(w, r, views.Reminders(views.RemindersData{
		Reminders: items,
		FormError: formError,
		Form:      form,
	}))
}

// formToScheduleParams mirrors tgstates.ReminderDraft.ToScheduleParams' validation
// rules (weekly days 0-6, day-of-month 1-31, interval-days >=1, HH:MM bounds),
// applied directly to submitted form values instead of a multi-step wizard draft.
func formToScheduleParams(form views.ReminderFormData, tzOffset int) (*pb.ScheduleParams, error) {
	if form.Hour < 0 || form.Hour > 23 || form.Minute < 0 || form.Minute > 59 {
		return nil, fmt.Errorf("время должно быть в формате ЧЧ:ММ")
	}

	sp := &pb.ScheduleParams{
		Hour:     int32(form.Hour),
		Minute:   int32(form.Minute),
		TzOffset: int32(tzOffset),
	}

	switch form.ScheduleType {
	case "daily":
		// No extra params.
	case "weekly":
		if len(form.Days) == 0 {
			return nil, fmt.Errorf("выберите хотя бы один день недели")
		}
		days := make([]int32, len(form.Days))
		for i, d := range form.Days {
			if d < 0 || d > 6 {
				return nil, fmt.Errorf("день недели должен быть от 0 (Пн) до 6 (Вс)")
			}
			days[i] = int32(d)
		}
		sp.Extra = &pb.ScheduleParams_Weekly{Weekly: &pb.ScheduleParams_WeeklyExtra{Days: days}}
	case "monthly":
		if form.DayOfMonth < 1 || form.DayOfMonth > 31 {
			return nil, fmt.Errorf("число месяца должно быть от 1 до 31")
		}
		sp.Extra = &pb.ScheduleParams_Monthly{Monthly: &pb.ScheduleParams_MonthlyExtra{DayOfMonth: int32(form.DayOfMonth)}}
	case "yearly":
		if form.Month < 1 || form.Month > 12 || form.Day < 1 || form.Day > 31 {
			return nil, fmt.Errorf("укажите корректные месяц (1-12) и день (1-31)")
		}
		sp.Extra = &pb.ScheduleParams_Yearly{Yearly: &pb.ScheduleParams_YearlyExtra{Month: int32(form.Month), Day: int32(form.Day)}}
	case "once":
		if form.Date == "" {
			return nil, fmt.Errorf("укажите дату")
		}
		sp.Extra = &pb.ScheduleParams_Once{Once: &pb.ScheduleParams_OnceExtra{Date: form.Date}}
	case "custom_days":
		if form.IntervalDays < 1 {
			return nil, fmt.Errorf("интервал должен быть положительным числом дней")
		}
		sp.Extra = &pb.ScheduleParams_CustomDays{CustomDays: &pb.ScheduleParams_CustomDaysExtra{IntervalDays: int32(form.IntervalDays)}}
	default:
		return nil, fmt.Errorf("неизвестный тип расписания")
	}

	return sp, nil
}

func parseReminderForm(r *http.Request) views.ReminderFormData {
	form := views.ReminderFormData{
		Title:        r.PostFormValue("title"),
		ScheduleType: r.PostFormValue("schedule_type"),
		Hour:         atoiOr(r.PostFormValue("hour"), -1),
		Minute:       atoiOr(r.PostFormValue("minute"), -1),
		DayOfMonth:   atoiOr(r.PostFormValue("day_of_month"), 0),
		Month:        atoiOr(r.PostFormValue("month"), 0),
		Day:          atoiOr(r.PostFormValue("day"), 0),
		Date:         r.PostFormValue("date"),
		IntervalDays: atoiOr(r.PostFormValue("interval_days"), 0),
		CreateTask:   r.PostFormValue("create_task") != "",
	}
	for _, d := range r.PostForm["days"] {
		if n, err := strconv.Atoi(d); err == nil {
			form.Days = append(form.Days, n)
		}
	}
	return form
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func (a *App) handleCreateReminder(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	form := parseReminderForm(r)
	a.createReminder(w, r, form)
}

func (a *App) createReminder(w http.ResponseWriter, r *http.Request, form views.ReminderFormData) {
	scheduleParams, err := formToScheduleParams(form, a.Cfg.TimezoneOffsetHours)
	if err != nil {
		a.renderReminders(w, r, err.Error(), form)
		return
	}

	title := form.Title
	if title == "" {
		title = "Напоминание"
	}
	scheduleType := form.ScheduleType
	if scheduleType == "" {
		scheduleType = "daily"
	}

	_, err = a.Notifications.CreateReminder(r.Context(), a.Cfg.RootID, title, scheduleType, scheduleParams, form.CreateTask)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.InvalidArgument {
			a.renderReminders(w, r, "Выбранное время уже прошло — введите другое", form)
			return
		}
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/reminders", http.StatusSeeOther)
}

func (a *App) handleCreateReminderNL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	text := r.PostFormValue("text")
	if text == "" {
		a.renderReminders(w, r, "Введите описание напоминания", views.ReminderFormData{ScheduleType: "daily"})
		return
	}

	ctx := r.Context()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	logical := timeutil.LogicalToday(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour)
	const iso = "2006-01-02"
	result, err := a.LLM.ParseReminder(ctx, text,
		now.Format("2006-01-02 15:04"),
		logical.Format(iso),
		logical.AddDate(0, 0, 1).Format(iso),
		logical.AddDate(0, 0, 2).Format(iso),
	)
	if err != nil {
		a.renderReminders(w, r, "Не удалось разобрать напоминание, заполните форму вручную", views.ReminderFormData{ScheduleType: "daily"})
		return
	}

	form := views.ReminderFormData{
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
	if form.ScheduleType == "" {
		form.ScheduleType = "daily"
	}
	a.renderReminders(w, r, "", form)
}

func (a *App) handleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.formError(w, r, "Некорректный идентификатор напоминания")
		return
	}
	if _, err := a.Notifications.DeleteReminder(r.Context(), id, a.Cfg.RootID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/reminders", http.StatusSeeOther)
}

func (a *App) handlePostponeReminder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.formError(w, r, "Некорректный идентификатор напоминания")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}

	var minutes int32
	if duration := strings.TrimSpace(r.PostFormValue("duration")); duration != "" {
		n, err := parseDuration(duration)
		if err != nil {
			a.renderReminders(w, r, err.Error(), views.ReminderFormData{ScheduleType: "daily"})
			return
		}
		minutes = int32(n)
	} else {
		date := r.PostFormValue("postpone_date")
		timeStr := r.PostFormValue("postpone_time")
		parts := strings.SplitN(timeStr, ":", 2)
		if date == "" || len(parts) != 2 {
			a.renderReminders(w, r, "Укажите длительность или дату и время переноса", views.ReminderFormData{ScheduleType: "daily"})
			return
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			a.renderReminders(w, r, "Введите время в формате ЧЧ:ММ", views.ReminderFormData{ScheduleType: "daily"})
			return
		}
		loc := time.FixedZone("tz", a.Cfg.TimezoneOffsetHours*3600)
		d, err := time.ParseInLocation("2006-01-02", date, loc)
		if err != nil {
			a.renderReminders(w, r, "Некорректная дата", views.ReminderFormData{ScheduleType: "daily"})
			return
		}
		target := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
		minutesUntil := int32(time.Until(target).Minutes())
		if minutesUntil < 1 {
			a.renderReminders(w, r, "Выбранное время уже прошло", views.ReminderFormData{ScheduleType: "daily"})
			return
		}
		minutes = minutesUntil
	}

	if _, err := a.Notifications.PostponeReminder(r.Context(), id, a.Cfg.RootID, minutes); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/reminders", http.StatusSeeOther)
}

// parseDuration parses a human-readable duration string into total minutes.
// Copied from frontends/telegram/tghandlers/reminder_postpone.go (unexported
// there) — supports m/h/d/w/M units (e.g. "2h30m", "1d12h") or a bare integer
// of minutes.
func parseDuration(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("введите положительное значение")
		}
		return n, nil
	}

	s = strings.ReplaceAll(s, " ", "")

	vals := make(map[byte]int)
	i := 0
	for i < len(s) {
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

	if m, ok := vals['m']; ok && m >= 60 {
		return 0, fmt.Errorf("%dm — это больше часа; используйте h/d/w", m)
	}
	if h, ok := vals['h']; ok && h >= 24 {
		return 0, fmt.Errorf("%dh — это больше суток; используйте d/w", h)
	}
	if d, ok := vals['d']; ok && d >= 7 {
		return 0, fmt.Errorf("%dd — это больше недели; используйте w", d)
	}

	total := vals['m'] + vals['h']*60 + vals['d']*1440 + vals['w']*10080 + vals['M']*43200
	if total <= 0 {
		return 0, fmt.Errorf("введите положительное значение")
	}
	return total, nil
}
