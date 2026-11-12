package tghandlers

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/automation/v1"
)

func (a *App) handleAutomationAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	if a.Automations == nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationUnavailable), nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	uc, err := a.State.GetContext(ctx, user)
	if err != nil {
		return err
	}
	arg := ""
	if len(parts) > 2 {
		arg = parts[2]
	}
	set := func(state tgstates.UserState, draft tgstates.AutomationDraft) error {
		return a.State.UpdateContext(ctx, user, func(u *tgstates.UserContext) { u.State = state; u.AutomationDraft = draft })
	}
	switch parts[1] {
	case "list":
		out, err := a.Automations.ListRules(ctx, &pb.ListRulesRequest{UserId: user})
		if err != nil {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationUnavailable), nil)
		}
		if err = set(tgstates.StateIdle, tgstates.AutomationDraft{}); err != nil {
			return err
		}
		page, _ := strconv.Atoi(arg)
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationList), tgkeyboards.AutomationRules(out.Rules, page))
	case "new":
		tasks, err := a.Core.GetTasks(ctx, uc.ActiveDate)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationNoTasks), tgkeyboards.AutomationCancel())
		}
		d := tgstates.AutomationDraft{Date: uc.ActiveDate, RequestID: uuid.NewString()}
		for _, t := range tasks {
			d.Choices = append(d.Choices, tgstates.AutomationChoice{Index: t.Index, Text: t.Text})
		}
		if err = set(tgstates.StateAutomationTask, d); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationChoose), tgkeyboards.AutomationTasks(d.Choices, 0))
	case "tasks":
		if uc.State != tgstates.StateAutomationTask {
			return nil
		}
		page, _ := strconv.Atoi(arg)
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationChoose), tgkeyboards.AutomationTasks(uc.AutomationDraft.Choices, page))
	case "task":
		if uc.State != tgstates.StateAutomationTask {
			return nil
		}
		index, err := strconv.Atoi(arg)
		if err != nil {
			return nil
		}
		d := uc.AutomationDraft
		expected := ""
		found := false
		for _, t := range d.Choices {
			if t.Index == index {
				expected = t.Text
				found = true
			}
		}
		if !found {
			return nil
		}
		tracker, ok := a.Core.(clients.TrackedTaskService)
		if !ok {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationUnavailable), nil)
		}
		task, err := tracker.TrackTask(ctx, d.Date, index, expected)
		if err != nil {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationChanged), tgkeyboards.AutomationCancel())
		}
		d.TaskID, d.TaskTitle = task.ID, task.Text
		d.Choices = nil
		if err = set(tgstates.StateAutomationStatus, d); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationStatusPrompt), tgkeyboards.AutomationStatuses())
	case "status":
		if uc.State != tgstates.StateAutomationStatus {
			return nil
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > 3 {
			return nil
		}
		d := uc.AutomationDraft
		d.TargetStatus = int32(n)
		if err = set(tgstates.StateAutomationText, d); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationTextPrompt), tgkeyboards.AutomationCancel())
	case "repeat":
		if uc.State != tgstates.StateAutomationRepeat || (arg != "once" && arg != "always") {
			return nil
		}
		d := uc.AutomationDraft
		d.Repeat = arg == "always"
		if err = set(tgstates.StateAutomationConfirm, d); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationConfirm(d.TaskTitle, d.Date, d.TargetStatus, d.DelaySeconds/60, d.Text, d.Repeat)), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoSave, "automation:save"), tgkeyboards.GeoButton(resources.GeoCancel, "automation:list:0")))
	case "save":
		if uc.State != tgstates.StateAutomationConfirm {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoStale), tgkeyboards.AutomationCancel())
		}
		d := uc.AutomationDraft
		_, err := a.Automations.CreateRule(ctx, &pb.CreateRuleRequest{UserId: user, RequestId: d.RequestID, Rule: &pb.Rule{Trigger: &pb.Rule_TaskStatus{TaskStatus: &pb.TaskTrigger{Date: d.Date, TaskId: d.TaskID, TargetStatus: pb.TaskStatus(d.TargetStatus)}}, Action: &pb.Rule_Notify{Notify: &pb.NotifyAction{Text: d.Text}}, DelaySeconds: d.DelaySeconds, Repeat: d.Repeat}})
		if err != nil {
			return err
		}
		if err = set(tgstates.StateIdle, tgstates.AutomationDraft{}); err != nil {
			return err
		}
		return a.handleAutomationAction(ctx, b, q, user, []string{"automation", "list", "0"})
	case "view":
		rules, err := a.Automations.ListRules(ctx, &pb.ListRulesRequest{UserId: user})
		if err != nil {
			return err
		}
		for _, r := range rules.Rules {
			if strconv.FormatInt(r.Id, 10) != arg {
				continue
			}
			t := r.GetTaskStatus()
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationConfirm(r.TaskTitle, t.Date, int32(t.TargetStatus), r.DelaySeconds/60, r.GetNotify().Text, r.Repeat)), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoDelete, "automation:confirmdelete:"+arg), tgkeyboards.GeoButton(resources.GeoBack, "automation:list:0")))
		}
		return nil
	case "confirmdelete":
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.AutomationDeletePrompt), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoDelete, "automation:delete:"+arg), tgkeyboards.GeoButton(resources.GeoCancel, "automation:list:0")))
	case "delete":
		id, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return nil
		}
		if _, err = a.Automations.DeleteRule(ctx, &pb.DeleteRuleRequest{UserId: user, RuleId: id}); err != nil {
			return err
		}
		return a.handleAutomationAction(ctx, b, q, user, []string{"automation", "list", "0"})
	case "history":
		out, err := a.Automations.ListExecutions(ctx, &pb.ListExecutionsRequest{UserId: user})
		if err != nil {
			return err
		}
		var lines []string
		for _, e := range out.Executions[:min(10, len(out.Executions))] {
			lines = append(lines, resources.AutomationExecution(e.TaskTitle, e.Status, e.Attempts, e.DueAt.AsTime().In(time.FixedZone("", a.Cfg.TimezoneOffsetHours*3600)).Format("02.01 15:04")))
		}
		text := strings.Join(lines, "\n")
		if text == "" {
			text = resources.AutomationNoHistory
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(text), tgkeyboards.AutomationCancel())
	}
	return nil
}
func automationStateText(a *App, ctx context.Context, b *telego.Bot, _ *telego.Update, chat, user int64, text string, uc *tgstates.UserContext) {
	d := uc.AutomationDraft
	next := uc.State
	prompt := ""
	kb := tgkeyboards.AutomationCancel()
	switch uc.State {
	case tgstates.StateAutomationText:
		if n := utf8.RuneCountInString(text); n > 0 && n <= 500 {
			d.Text = text
			next = tgstates.StateAutomationDelay
			prompt = resources.AutomationDelayPrompt
		}
	case tgstates.StateAutomationDelay:
		n, err := strconv.Atoi(text)
		if err == nil && n >= 0 && n <= 43200 {
			d.DelaySeconds = int32(n * 60)
			next = tgstates.StateAutomationRepeat
			prompt = resources.AutomationRepeatPrompt
			kb = tgkeyboards.AutomationFrequency()
		}
	}
	if prompt == "" {
		prompt = resources.GeoInvalid
	} else if err := a.State.UpdateContext(ctx, user, func(u *tgstates.UserContext) { u.State = next; u.AutomationDraft = d }); err != nil {
		return
	}
	a.replyToStateMessage(ctx, b, chat, user, tgfmt.Escape(prompt), kb)
}
func (a *App) RunAutomationDeliveries(ctx context.Context, b *telego.Bot) {
	if a.Automations == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for i := 0; i < 10; i++ {
			found, err := a.deliverAutomation(ctx, b)
			if err != nil {
				if ctx.Err() == nil {
					a.Logger.Warn("automation delivery failed; retry retained")
				}
				break
			}
			if !found {
				break
			}
		}
	}
}
func (a *App) deliverAutomation(ctx context.Context, b *telego.Bot) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := a.Automations.ClaimDelivery(callCtx, &pb.ClaimDeliveryRequest{UserId: a.Cfg.RootID})
	cancel()
	if err != nil {
		return false, err
	}
	d := out.GetDelivery()
	if d == nil {
		return false, nil
	}
	return true, a.sendLeasedNotification(ctx, b, tgfmt.Escape(resources.AutomationNotice(d.Text)), d.ExpiresAt.AsTime(), func(ctx context.Context, success bool) error {
		_, err := a.Automations.CompleteDelivery(ctx, &pb.CompleteDeliveryRequest{UserId: a.Cfg.RootID, DeliveryId: d.Id, LeaseToken: d.LeaseToken, Success: success})
		return err
	})
}
