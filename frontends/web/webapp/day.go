package webapp

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.uber.org/zap"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/applog"
)

func (a *App) registerDayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /day", a.handleDayView)
	mux.HandleFunc("POST /day/rating", a.handleUpdateRating)
	mux.HandleFunc("POST /day/tasks", a.handleAddTask)
	mux.HandleFunc("POST /day/tasks/{index}/toggle", a.handleToggleTask)
	mux.HandleFunc("POST /day/append", a.handleAppendToNote)
}

// dayViewData loads a day note's content, rating, and tasks concurrently —
// mirrors the errgroup pattern used by the Telegram frontend's note view.
type dayViewData struct {
	Date      string
	Content   string
	Rating    int
	HasRating bool
	Tasks     []*taskView
}

type taskView struct {
	Index     int
	Text      string
	Completed bool
}

func (a *App) loadDayView(ctx context.Context, date string) (*dayViewData, error) {
	if _, err := a.Core.EnsureNote(ctx, date); err != nil {
		return nil, err
	}

	data := &dayViewData{Date: date}
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		content, err := a.Core.GetNote(gctx, date)
		if err != nil {
			return err
		}
		data.Content = content
		return nil
	})
	g.Go(func() error {
		rating, hasRating, err := a.Core.GetRating(gctx, date)
		if err != nil {
			return err
		}
		data.Rating, data.HasRating = rating, hasRating
		return nil
	})
	g.Go(func() error {
		tasks, err := a.Core.GetTasks(gctx, date)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			data.Tasks = append(data.Tasks, &taskView{Index: t.Index, Text: t.Text, Completed: t.Completed})
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *App) resolveDate(ctx context.Context, r *http.Request) (string, error) {
	if date := r.URL.Query().Get("date"); date != "" {
		return date, nil
	}
	return a.Core.GetTodayDate(ctx)
}

func (a *App) handleDayView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date, err := a.resolveDate(ctx, r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	data, err := a.loadDayView(ctx, date)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.Day(viewDayData(data)))
}

func viewDayData(d *dayViewData) views.DayData {
	tasks := make([]views.TaskData, len(d.Tasks))
	for i, t := range d.Tasks {
		tasks[i] = views.TaskData{Index: t.Index, Text: t.Text, Completed: t.Completed}
	}
	return views.DayData{
		Date:      d.Date,
		Content:   d.Content,
		Rating:    d.Rating,
		HasRating: d.HasRating,
		Tasks:     tasks,
	}
}

func (a *App) handleUpdateRating(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	date := r.PostFormValue("date")
	rating, err := strconv.Atoi(r.PostFormValue("rating"))
	if err != nil || rating < 0 || rating > 10 {
		a.formError(w, r, "Оценка должна быть числом от 0 до 10")
		return
	}
	if _, err := a.Core.UpdateRating(ctx, date, rating); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.reloadDayFragment(w, r, date)
}

func (a *App) handleAddTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	date := r.PostFormValue("date")
	text := r.PostFormValue("text")
	if text == "" {
		a.formError(w, r, "Текст задачи не может быть пустым")
		return
	}
	if _, err := a.Core.AddTask(ctx, date, text); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.reloadDayFragment(w, r, date)
}

func (a *App) handleToggleTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		a.formError(w, r, "Некорректный номер задачи")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	date := r.FormValue("date")
	if _, err := a.Core.ToggleTask(ctx, date, index); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.reloadDayFragment(w, r, date)
}

func (a *App) handleAppendToNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	date := r.PostFormValue("date")
	text := r.PostFormValue("text")
	if text == "" {
		a.formError(w, r, "Текст не может быть пустым")
		return
	}
	if _, err := a.Core.AppendToNote(ctx, date, text); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.reloadDayFragment(w, r, date)
}

// reloadDayFragment re-fetches the day view and renders the full page. htmx
// requests target the whole <main> via hx-select, so a full-page render is
// simplest and keeps every fragment consistent with GET /day.
func (a *App) reloadDayFragment(w http.ResponseWriter, r *http.Request, date string) {
	data, err := a.loadDayView(r.Context(), date)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, views.Day(viewDayData(data)))
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	applog.With(r.Context(), a.Logger).Error("handler error", zap.Error(err))
	if st, ok := status.FromError(errors.Unwrap(err)); ok && st.Code() == codes.Unavailable {
		http.Error(w, "Сервис временно недоступен, попробуйте позже", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, "Внутренняя ошибка, попробуйте ещё раз", http.StatusInternalServerError)
}

// formError surfaces a validation problem. For htmx requests it must respond
// with 200 + an out-of-band swap: htmx does not swap 4xx bodies, so a plain
// 422 would be invisible to the user. Non-htmx callers still get a 422.
func (a *App) formError(w http.ResponseWriter, r *http.Request, message string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if err := views.FormErrorOOB(message).Render(r.Context(), w); err != nil {
			applog.With(r.Context(), a.Logger).Error("render form error", zap.Error(err))
		}
		return
	}
	http.Error(w, message, http.StatusUnprocessableEntity)
}
