package tghandlers

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mymmrac/telego"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/voicejobs"
)

// bindVoiceKeyboard binds every mutating confirmation to its durable input.
// Copy the rows so binding one job cannot change a keyboard reused by another.
func (a *App) bindVoiceKeyboard(id string, kb telego.InlineKeyboardMarkup) telego.InlineKeyboardMarkup {
	opcodes := map[string]string{
		"smart:yes": "y", "smart:no": "n", "smart:pick:note": "pn",
		"smart:pick:task": "pt", "smart:pick:reminder": "pr",
		"reminder:nl_confirm": "rc", "reminder:cancel": "n",
	}
	bound := kb
	bound.InlineKeyboard = make([][]telego.InlineKeyboardButton, len(kb.InlineKeyboard))
	for row, buttons := range kb.InlineKeyboard {
		bound.InlineKeyboard[row] = append([]telego.InlineKeyboardButton(nil), buttons...)
		for column := range bound.InlineKeyboard[row] {
			button := &bound.InlineKeyboard[row][column]
			if opcode, ok := opcodes[button.CallbackData]; ok {
				button.CallbackData = "voice:do:" + id + ":" + opcode
			}
		}
	}
	return bound
}

// restoreVoiceDraft replaces the fields a generic confirmation handler reads.
// It works after Redis session expiration and clears drafts from another job.
// The caller holds the normal per-user callback lock.
func (a *App) restoreVoiceDraft(ctx context.Context, j *voicejobs.Job, p voicePrepared) error {
	return a.State.UpdateContext(ctx, j.UserID, func(u *tgstates.UserContext) {
		u.ActiveDate = j.ActiveDate
		u.SmartDraft = tgstates.SmartDraft{}
		u.ReminderDraft = tgstates.ReminderDraft{}
		if p.Reminder != nil {
			r := p.Reminder
			u.ReminderDraft = tgstates.ReminderDraft{Title: r.Title, ScheduleType: r.ScheduleType,
				Hour: r.Hour, Minute: r.Minute, Days: append([]int(nil), r.Days...), DayOfMonth: r.DayOfMonth,
				Month: r.Month, Day: r.Day, Date: r.Date, IntervalDays: r.IntervalDays, CreateTask: r.CreateTask}
		}
		if p.Intent != nil {
			r := p.Intent
			u.State = tgstates.StateSmartConfirm
			u.SmartDraft = tgstates.SmartDraft{RawText: j.Text, Intent: r.Intent, Confidence: r.Confidence, TaskTitle: r.Title}
		} else {
			u.State = tgstates.StateReminderCreateTaskConfirm
		}
	})
}

func (a *App) handleDurableVoiceConfirmation(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, id, opcode string) error {
	if a.VoiceJobs == nil || !a.authorized(user) || q == nil || q.Message == nil {
		return nil
	}
	switch opcode {
	case "y", "n", "pn", "pt", "pr", "rc":
	default:
		return nil
	}
	j, err := a.VoiceJobs.Get(ctx, id, user)
	if err != nil || j == nil {
		return err
	}
	if j.UserID != user || q.Message.GetChat().ID != j.ChatID {
		return nil
	}
	var p voicePrepared
	if err := json.Unmarshal(j.Prepared, &p); err != nil {
		return err
	}
	if p.Intent == nil && p.Reminder == nil {
		return errors.New("missing prepared voice confirmation")
	}
	if opcode == "pr" && p.Reminder == nil {
		if p.Intent == nil || p.Intent.Intent != clients.IntentUnknown && p.Intent.Intent != clients.IntentReminder {
			return nil
		}
		// Manual selection interprets relative dates at original acceptance
		// time. A dedicated CAS extends the finalized prepared snapshot; this
		// is not an unleased Save of worker state.
		reminderJob := *j
		reminderJob.Mode = "reminder"
		prepared, err := a.prepareVoiceIntent(ctx, &reminderJob)
		if err != nil {
			return err
		}
		var reminder voicePrepared
		if err = json.Unmarshal(prepared, &reminder); err != nil {
			return err
		}
		p.Reminder = reminder.Reminder
		prepared, err = json.Marshal(p)
		if err != nil {
			return err
		}
		store, ok := a.VoiceJobs.(interface {
			SetPrepared(context.Context, string, int64, json.RawMessage, json.RawMessage) (bool, error)
		})
		if !ok {
			return errors.New("durable voice preparation storage unavailable")
		}
		stored, err := store.SetPrepared(ctx, j.ID, user, j.Prepared, prepared)
		if err != nil {
			return err
		}
		if stored {
			j.Prepared = prepared
		} else {
			// Another confirmation may already have persisted its parse.
			// Render and execute that winner, never a discarded local parse.
			j, err = a.VoiceJobs.Get(ctx, id, user)
			if err != nil || j == nil {
				return err
			}
			if j.UserID != user || j.ChatID != q.Message.GetChat().ID {
				return nil
			}
			p = voicePrepared{}
			if err = json.Unmarshal(j.Prepared, &p); err != nil {
				return err
			}
			if p.Reminder == nil {
				return errors.New("voice reminder preparation changed")
			}
		}
	}
	if err := a.restoreVoiceDraft(ctx, j, p); err != nil {
		return err
	}
	if opcode == "pr" {
		if p.Reminder == nil {
			return errors.New("missing parsed voice reminder")
		}
		kb := a.bindVoiceKeyboard(j.ID, tgkeyboards.NLReminderConfirm())
		return replyToCallback(ctx, b, q, formatNLReminderPreview(p.Reminder), &kb)
	}
	if opcode == "rc" {
		if p.Reminder == nil {
			return nil
		}
		a.HandleReminderNLConfirm(ctx, b, q, user)
		return nil
	}
	if opcode == "n" {
		a.HandleSmartReject(ctx, b, q, user)
		return nil
	}
	if p.Intent == nil {
		return nil
	}
	intent := p.Intent.Intent
	switch opcode {
	case "pn":
		intent = clients.IntentNote
	case "pt":
		intent = clients.IntentTask
	}
	if intent == clients.IntentNote {
		core, ok := a.Core.(clients.IdempotentNoteService)
		if !ok {
			return errors.New("idempotent note service required")
		}
		saved, err := core.AppendToNoteOnce(ctx, j.ActiveDate, j.Text, "voice:"+j.ID)
		if err != nil {
			return err
		}
		if !saved {
			return errors.New("voice note confirmation rejected")
		}
		a.resetSmart(ctx, user)
		kb := a.getMainMenuKeyboard(ctx)
		return replyToCallback(ctx, b, q, tgfmt.Join(tgfmt.Escape(resources.VoiceSaved+" "), tgfmt.Code(tgfmt.Escape(j.ActiveDate))), &kb)
	}
	if opcode == "y" {
		a.HandleSmartConfirm(ctx, b, q, user)
	} else {
		a.HandleSmartPickIntent(ctx, b, q, user, intent)
	}
	return nil
}
