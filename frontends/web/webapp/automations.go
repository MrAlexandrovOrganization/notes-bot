package webapp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/web/views"
	pb "notes-bot/proto/automation/v1"
)

func (a *App) registerAutomationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /automations", a.handleAutomations)
	mux.HandleFunc("POST /automations/track", a.handleTrackAutomation)
	mux.HandleFunc("POST /automations", a.handleCreateAutomation)
	mux.HandleFunc("POST /automations/{id}/delete", a.handleDeleteAutomation)
	mux.HandleFunc("POST /automations/executions/{id}/cancel", a.handleCancelAutomationExecution)
}
func (a *App) automationForm(w http.ResponseWriter, r *http.Request) bool {
	if a.Automations == nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return false
	}
	return true
}
func (a *App) handleAutomations(w http.ResponseWriter, r *http.Request) {
	if a.Automations == nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	date, err := a.resolveDate(ctx, r)
	if err != nil {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	data := views.AutomationsData{Date: date, RequestID: uuid.NewString()}
	data.Tasks, err = a.Core.GetTasks(ctx, date)
	if err != nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	out, err := a.Automations.ListRules(ctx, &pb.ListRulesRequest{UserId: a.Cfg.RootID})
	if err != nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	data.Rules = out.Rules
	runs, err := a.Automations.ListExecutions(ctx, &pb.ListExecutionsRequest{UserId: a.Cfg.RootID})
	if err != nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	for _, e := range runs.Executions {
		due := e.DueAt.AsTime().In(time.FixedZone("", a.Cfg.TimezoneOffsetHours*3600)).Format("02.01 15:04")
		data.Executions = append(data.Executions, views.AutomationExecution{
			ID: e.Id, Label: resources.AutomationExecution(e.TaskTitle, e.Status, e.ActionType, e.Attempts, due),
			CanCancel: e.Status == "pending" && e.Attempts == 0,
		})
	}
	if id := r.URL.Query().Get("task"); id != "" {
		for _, task := range data.Tasks {
			if task.ID == id {
				data.Selected = task
			}
		}
		if data.Selected == nil {
			http.Error(w, resources.AutomationChanged, http.StatusConflict)
			return
		}
	}
	a.render(w, r, views.Automations(data))
}
func (a *App) handleTrackAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.automationForm(w, r) {
		return
	}
	tracker, ok := a.Core.(clients.TrackedTaskService)
	if !ok {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	index, err := strconv.Atoi(r.FormValue("index"))
	if err != nil || index < 0 {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	task, err := tracker.TrackTask(ctx, r.FormValue("date"), index, r.FormValue("expected"))
	if err != nil {
		http.Error(w, resources.AutomationChanged, http.StatusConflict)
		return
	}
	query := url.Values{"date": {r.FormValue("date")}, "task": {task.ID}}
	http.Redirect(w, r, "/automations?"+query.Encode(), http.StatusSeeOther)
}
func (a *App) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.automationForm(w, r) {
		return
	}
	minutes, e1 := strconv.Atoi(r.FormValue("minutes"))
	state, e2 := strconv.Atoi(r.FormValue("status"))
	if e1 != nil || e2 != nil || minutes < 0 || minutes > 43200 || state < 1 || state > 3 {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	rule := &pb.Rule{
		Trigger:      &pb.Rule_TaskStatus{TaskStatus: &pb.TaskTrigger{Date: r.FormValue("date"), TaskId: r.FormValue("task_id"), TargetStatus: pb.TaskStatus(state)}},
		DelaySeconds: int32(minutes * 60), Repeat: r.FormValue("repeat") == "yes",
	}
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		rule.Action = &pb.Rule_Notify{Notify: &pb.NotifyAction{Text: text}}
	}
	if task := strings.TrimSpace(r.FormValue("task_text")); task != "" {
		rule.CreateTask = &pb.CreateTaskAction{Text: task}
	}
	if rule.Action == nil && rule.CreateTask == nil {
		http.Error(w, resources.AutomationNoActions, http.StatusBadRequest)
		return
	}
	_, err := a.Automations.CreateRule(ctx, &pb.CreateRuleRequest{UserId: a.Cfg.RootID, RequestId: r.FormValue("request_id"), Rule: rule})
	if err != nil {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/automations", http.StatusSeeOther)
}
func (a *App) handleCancelAutomationExecution(w http.ResponseWriter, r *http.Request) {
	if !a.automationForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = a.Automations.CancelExecution(ctx, &pb.CancelExecutionRequest{UserId: a.Cfg.RootID, ExecutionId: id})
	if status.Code(err) == codes.FailedPrecondition {
		http.Error(w, resources.AutomationAlreadyStarted, http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/automations", http.StatusSeeOther)
}

func (a *App) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	if !a.automationForm(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, resources.GeoInvalid, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err = a.Automations.DeleteRule(ctx, &pb.DeleteRuleRequest{UserId: a.Cfg.RootID, RuleId: id}); err != nil {
		http.Error(w, resources.AutomationUnavailable, http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "/automations", http.StatusSeeOther)
}
