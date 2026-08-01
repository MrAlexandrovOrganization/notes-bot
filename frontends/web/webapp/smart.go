package webapp

import (
	"net/http"
	"strings"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/timeutil"
)

// confidenceThreshold mirrors smart.go: below this, force intent to unknown
// and let the user pick manually.
const confidenceThreshold = 0.6

func (a *App) registerSmartRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /smart", a.handleSmartPage)
	mux.HandleFunc("POST /smart", a.handleSmartClassify)
	mux.HandleFunc("POST /smart/confirm", a.handleSmartConfirm)
}

func (a *App) handleSmartPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, views.Smart(views.SmartData{}))
}

func (a *App) handleSmartClassify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	text := strings.TrimSpace(r.PostFormValue("text"))
	if text == "" {
		a.render(w, r, views.Smart(views.SmartData{Error: "Введите текст"}))
		return
	}

	ctx := r.Context()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	logical := timeutil.LogicalToday(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour)
	const iso = "2006-01-02"
	currentDateTime := now.Format("2006-01-02 15:04")
	today := logical.Format(iso)
	tomorrow := logical.AddDate(0, 0, 1).Format(iso)
	dayAfter := logical.AddDate(0, 0, 2).Format(iso)

	result, err := a.LLM.ClassifyIntent(ctx, text, currentDateTime)
	if err != nil {
		a.render(w, r, views.Smart(views.SmartData{RawText: text, Error: "Не удалось обработать запрос"}))
		return
	}

	intent := result.Intent
	if result.Confidence < confidenceThreshold {
		intent = clients.IntentUnknown
	}

	var reminder *clients.LLMReminderResult
	if intent == clients.IntentReminder {
		rr, err := a.LLM.ParseReminder(ctx, text, currentDateTime, today, tomorrow, dayAfter)
		if err != nil {
			intent = clients.IntentUnknown
		} else {
			reminder = rr
		}
	}

	data := views.SmartData{
		RawText:    text,
		Intent:     intent,
		TaskTitle:  result.Title,
		Confidence: result.Confidence,
	}
	if reminder != nil {
		data.Reminder = &views.ReminderFormData{
			Title:        reminder.Title,
			ScheduleType: reminder.ScheduleType,
			Hour:         reminder.Hour,
			Minute:       reminder.Minute,
			Days:         reminder.Days,
			DayOfMonth:   reminder.DayOfMonth,
			Month:        reminder.Month,
			Day:          reminder.Day,
			Date:         reminder.Date,
			IntervalDays: reminder.IntervalDays,
			CreateTask:   reminder.CreateTask,
		}
	}
	a.render(w, r, views.Smart(data))
}

func (a *App) handleSmartConfirm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	ctx := r.Context()
	intent := r.PostFormValue("intent")
	rawText := r.PostFormValue("raw_text")

	switch intent {
	case clients.IntentNote:
		date, err := a.Core.GetTodayDate(ctx)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if _, err := a.Core.AppendToNote(ctx, date, rawText); err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/day", http.StatusSeeOther)

	case clients.IntentTask:
		title := r.PostFormValue("task_title")
		if title == "" {
			title = rawText
		}
		date, err := a.Core.GetTodayDate(ctx)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if _, err := a.Core.AddTask(ctx, date, title); err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/day", http.StatusSeeOther)

	case clients.IntentReminder:
		form := parseReminderForm(r)
		a.createReminder(w, r, form)

	default:
		a.render(w, r, views.Smart(views.SmartData{RawText: rawText, Error: "Не поняла, что сделать — выберите вручную"}))
	}
}
