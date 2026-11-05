package webapp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/timeutil"
	notespb "notes-bot/proto/notes"
)

// NewMobileRouter is served by a separate VPN-bound instance of /web. The public
// website does not register these routes. HTTP is appropriate at this native
// edge: Android uses the platform TLS client, while internal calls remain gRPC.
func (a *App) NewMobileRouter() http.Handler {
	protected := http.NewServeMux()
	protected.HandleFunc("GET /mobile/v1/bootstrap", a.mobileBootstrap)
	protected.HandleFunc("GET /mobile/v1/day", a.mobileDay)
	protected.HandleFunc("GET /mobile/v1/directory", a.mobileDirectory)
	protected.HandleFunc("GET /mobile/v1/note", a.mobileNote)
	protected.HandleFunc("POST /mobile/v1/append", a.mobileAppend)
	protected.HandleFunc("POST /mobile/v1/rating", a.mobileRating)
	protected.HandleFunc("POST /mobile/v1/task", a.mobileTask)
	protected.HandleFunc("GET /mobile/v1/search", a.mobileSearch)
	protected.HandleFunc("POST /mobile/v1/ask", a.mobileAsk)
	protected.HandleFunc("GET /mobile/v1/reminders", a.mobileReminders)
	protected.HandleFunc("POST /mobile/v1/reminders", a.mobileCreateReminder)
	protected.HandleFunc("POST /mobile/v1/reminders/action", a.mobileReminderAction)
	protected.HandleFunc("DELETE /mobile/v1/session", a.mobileLogout)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /mobile/v1/session", a.mobileLogin)
	mux.Handle("/mobile/", a.mobileAuth(protected))
	return a.logRequests(mux)
}

func mobileJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func mobileError(w http.ResponseWriter, code int) {
	mobileJSON(w, code, map[string]int{"error": code})
}

func mobileDecode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		mobileError(w, http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		mobileError(w, http.StatusBadRequest)
		return false
	}
	return true
}

func mobileFailure(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = http.StatusBadRequest
	case codes.NotFound:
		code = http.StatusNotFound
	case codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		code = http.StatusConflict
	}
	// Never include gRPC error text: it can contain note contents or paths.
	mobileError(w, code)
}

func (a *App) mobileLogin(w http.ResponseWriter, r *http.Request) {
	if a.tooManyLoginAttempts(clientIP(r)) {
		mobileError(w, http.StatusTooManyRequests)
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Password), []byte(a.Cfg.WebPassword)) != 1 {
		a.recordLoginFailure(clientIP(r))
		mobileError(w, http.StatusUnauthorized)
		return
	}
	token := a.signSession("android-"+newSessionID(), time.Now().Add(sessionTTL))
	mobileJSON(w, http.StatusOK, map[string]string{"token": token})
}

func mobileToken(r *http.Request) string {
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return value
}

func (a *App) mobileAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sameOrigin(r) != nil {
			mobileError(w, http.StatusForbidden)
			return
		}
		token := mobileToken(r)
		tok, ok := a.parseSession(token)
		if !ok || !strings.HasPrefix(tok.ID, "android-") || !a.verifySession(token) {
			mobileError(w, http.StatusUnauthorized)
			return
		}
		// Fixed lifetime; logout revokes this exact token. Reauthentication keeps
		// all private SQLite data and pending operations on the device.
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) mobileLogout(w http.ResponseWriter, r *http.Request) {
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: mobileToken(r)})
	a.revokeSession(r)
	mobileJSON(w, http.StatusOK, map[string]bool{"stored": true})
}

func (a *App) mobileBootstrap(w http.ResponseWriter, r *http.Request) {
	date, err := a.Core.GetTodayDate(r.Context())
	if err != nil {
		mobileFailure(w, err)
		return
	}
	dates, err := a.Core.GetExistingDates(r.Context())
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"today": date, "dates": dates,
		"timezone_offset": a.Cfg.TimezoneOffsetHours, "day_start_hour": a.Cfg.DayStartHour})
}

func (a *App) mobileDay(w http.ResponseWriter, r *http.Request) {
	date, err := a.resolveDate(r.Context(), r)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	// Read does not create files; an offline append explicitly ensures its day.
	content, err := a.Core.GetNote(r.Context(), date)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	tasks, err := a.Core.GetTasks(r.Context(), date)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	rating, hasRating, err := a.Core.GetRating(r.Context(), date)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"date": date, "content": content,
		"tasks": tasks, "rating": rating, "has_rating": hasRating})
}

func (a *App) mobileDirectory(w http.ResponseWriter, r *http.Request) {
	entries, err := a.Core.ListDirectory(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (a *App) mobileNote(w http.ResponseWriter, r *http.Request) {
	content, err := a.Core.GetNoteByPath(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]string{"content": content})
}

var mobileOperationID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (a *App) mobileAppend(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID   string `json:"id"`
		Date string `json:"date"`
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if !mobileOperationID.MatchString(request.ID) || strings.TrimSpace(request.Text) == "" ||
		(request.Date == "") == (request.Path == "") {
		mobileError(w, http.StatusBadRequest)
		return
	}
	var stored bool
	var err error
	if request.Path != "" {
		client, ok := a.Core.(clients.IdempotentNoteByPathService)
		if !ok {
			mobileError(w, http.StatusServiceUnavailable)
			return
		}
		stored, err = client.AppendToNoteByPathOnce(r.Context(), request.Path, request.Text, "android:"+request.ID)
	} else {
		client, ok := a.Core.(clients.IdempotentNoteService)
		if !ok {
			mobileError(w, http.StatusServiceUnavailable)
			return
		}
		if _, err = a.Core.EnsureNote(r.Context(), request.Date); err == nil {
			stored, err = client.AppendToNoteOnce(r.Context(), request.Date, request.Text, "android:"+request.ID)
		}
	}
	if err != nil {
		mobileFailure(w, err)
		return
	}
	if !stored {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"id": request.ID, "stored": true})
}

func (a *App) mobileRating(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Date   string `json:"date"`
		Rating int    `json:"rating"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if request.Rating < 0 || request.Rating > 10 {
		mobileError(w, 400)
		return
	}
	stored, err := a.Core.UpdateRating(r.Context(), request.Date, request.Rating)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]bool{"stored": stored})
}

func (a *App) mobileTask(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Date  string `json:"date"`
		Index int    `json:"index"`
		State int    `json:"state"`
		Text  string `json:"text"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if request.State < 0 || request.State > 2 || request.Index < 0 {
		mobileError(w, 400)
		return
	}
	// Task indices are positional: refuse an outdated selection. This endpoint
	// is online-only; durable additions use the idempotent append endpoint.
	tasks, err := a.Core.GetTasks(r.Context(), request.Date)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	found := false
	for _, task := range tasks {
		if task.Index == request.Index && task.Text == request.Text {
			found = true
		}
	}
	if !found {
		mobileError(w, http.StatusConflict)
		return
	}
	stored, err := a.Core.SetTaskStatus(r.Context(), request.Date, request.Index, notespb.TaskState(request.State))
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]bool{"stored": stored})
}

func (a *App) mobileSearch(w http.ResponseWriter, r *http.Request) {
	hits, err := a.searchNotes(r.Context(), r.URL.Query().Get("q"), searchLimit)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func (a *App) mobileAsk(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Question string `json:"question"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Question) == "" {
		mobileError(w, 400)
		return
	}
	result, err := a.Search.AskNotes(r.Context(), request.Question,
		timeutil.LocalNow(a.Cfg.TimezoneOffsetHours).Format("2006-01-02 15:04"), clients.SearchOptions{})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"answer": result.Answer, "sources": result.Evidence})
}

func (a *App) mobileReminders(w http.ResponseWriter, r *http.Request) {
	reminders, err := a.Notifications.ListReminders(r.Context(), a.Cfg.RootID)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"reminders": reminders})
}

func (a *App) mobileCreateReminder(w http.ResponseWriter, r *http.Request) {
	var form views.ReminderFormData
	if !mobileDecode(w, r, &form) {
		return
	}
	params, err := formToScheduleParams(form, a.Cfg.TimezoneOffsetHours)
	if err != nil || strings.TrimSpace(form.Title) == "" {
		mobileError(w, 400)
		return
	}
	reminder, err := a.Notifications.CreateReminder(r.Context(), a.Cfg.RootID, form.Title, form.ScheduleType, params, form.CreateTask)
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"reminder": reminder})
}

func (a *App) mobileReminderAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID      int64  `json:"id"`
		Action  string `json:"action"`
		Minutes int32  `json:"minutes"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if request.ID <= 0 {
		mobileError(w, 400)
		return
	}
	var err error
	switch request.Action {
	case "delete":
		_, err = a.Notifications.DeleteReminder(r.Context(), request.ID, a.Cfg.RootID)
	case "postpone":
		if request.Minutes < 1 || request.Minutes > 525600 {
			mobileError(w, 400)
			return
		}
		_, err = a.Notifications.PostponeReminder(r.Context(), request.ID, a.Cfg.RootID, request.Minutes)
	default:
		mobileError(w, 400)
		return
	}
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]bool{"stored": true})
}
