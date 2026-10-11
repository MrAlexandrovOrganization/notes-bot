package tghandlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"

	metricsbot "notes-bot/frontends/telegram/bot"
	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/voicejobs"
	pb "notes-bot/proto/notifications"
)

type confirmationPreparedRepo struct {
	*durableVoiceRepo
	setError error
	winner   json.RawMessage
	sets     int
}

func (r *confirmationPreparedRepo) SetPrepared(_ context.Context, id string, user int64, old, next json.RawMessage) (bool, error) {
	r.sets++
	if r.setError != nil {
		return false, r.setError
	}
	j := r.jobs[id]
	if j == nil || j.UserID != user || !r.done[id] {
		return false, nil
	}
	if len(r.winner) != 0 {
		j.Prepared = append(json.RawMessage(nil), r.winner...)
		return false, nil
	}
	if !bytes.Equal(j.Prepared, old) {
		return false, nil
	}
	j.Prepared = append(json.RawMessage(nil), next...)
	return true, nil
}

type confirmationNotifications struct {
	clients.NotificationsService
	titles []string
	dates  []string
}

func (n *confirmationNotifications) CreateReminder(_ context.Context, _ int64, title, _ string, params *pb.ScheduleParams, _ bool) (*clients.ReminderInfo, error) {
	n.titles = append(n.titles, title)
	n.dates = append(n.dates, params.GetOnce().GetDate())
	return &clients.ReminderInfo{Title: title, NextFireAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}, nil
}

func (n *confirmationNotifications) ListReminders(context.Context, int64) ([]*clients.ReminderInfo, error) {
	return nil, nil
}

type confirmationTaskCore struct {
	*durableVoiceCore
	date, title string
}

func (c *confirmationTaskCore) AddTask(_ context.Context, date, title string) (bool, error) {
	c.date, c.title = date, title
	return true, nil
}

func confirmationJob(t *testing.T, id, mode, text string, prepared voicePrepared) *voicejobs.Job {
	t.Helper()
	j := durableVoiceJob(id, 1)
	j.Mode, j.Text = mode, text
	j.ResultReady = true
	var err error
	j.Prepared, err = json.Marshal(prepared)
	require.NoError(t, err)
	return j
}

func confirmationQuery(j *voicejobs.Job) *telego.CallbackQuery {
	return &telego.CallbackQuery{Message: &telego.Message{MessageID: j.StatusMessageID, Chat: telego.Chat{ID: j.ChatID}}}
}

func TestVoiceConfirmationButtonsBindJobsAndFitTelegramLimit(t *testing.T) {
	a := &App{}
	id := uuid.NewString()
	for _, keyboard := range []telego.InlineKeyboardMarkup{tgkeyboards.SmartConfirm(), tgkeyboards.SmartIntentPicker(), tgkeyboards.NLReminderConfirm()} {
		original, err := json.Marshal(keyboard)
		require.NoError(t, err)
		bound := a.bindVoiceKeyboard(id, keyboard)
		for _, row := range bound.InlineKeyboard {
			for _, button := range row {
				require.LessOrEqual(t, len([]byte(button.CallbackData)), 64)
				if button.CallbackData != "reminder:create" {
					require.Contains(t, button.CallbackData, "voice:do:"+id+":")
				}
			}
		}
		after, err := json.Marshal(keyboard)
		require.NoError(t, err)
		require.Equal(t, original, after, "binding must not mutate the input keyboard")
	}
}

func TestVoiceConfirmationAAfterOpeningBUsesAAndAppendOnce(t *testing.T) {
	r, w, c, _, b := durableVoiceFixture(t)
	a := durableVoiceApp(r, w, c)
	first := confirmationJob(t, uuid.NewString(), "smart", "текст A <&>", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentNote, Confidence: 0.99}})
	first.ActiveDate = "01-Oct-2026"
	second := confirmationJob(t, uuid.NewString(), "smart", "текст B", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentNote, Confidence: 0.99}})
	second.ActiveDate = "02-Oct-2026"
	for _, j := range []*voicejobs.Job{first, second} {
		r.jobs[j.ID] = cloneDurableVoice(j)
		require.NoError(t, a.openDurableVoice(t.Context(), b, confirmationQuery(j), j.UserID, j.ID))
	}
	require.Equal(t, second.Text, a.State.(*noteFlowState).value.SmartDraft.RawText)
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(first), first.UserID, first.ID, "y"))
	require.Equal(t, first.ActiveDate+"\x00"+first.Text, c.operations["voice:"+first.ID])
	require.NotContains(t, c.operations, "voice:"+second.ID)
	require.Equal(t, first.ActiveDate, a.State.(*noteFlowState).value.ActiveDate)
	// Simulate Redis TTL expiry and duplicate delivery of the old callback.
	a.State.(*noteFlowState).value = tgstates.UserContext{UserID: 42, ActiveDate: "01-Jan-2030"}
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(first), first.UserID, first.ID, "y"))
	require.Len(t, c.operations, 1)
	require.Equal(t, []string{"voice:" + first.ID, "voice:" + first.ID}, c.calls)
	require.Equal(t, first.Prepared, r.jobs[first.ID].Prepared)
	require.Empty(t, r.events, "confirmation must never call unleased worker Save")
}

func TestVoiceConfirmationTaskUsesJobTitleAndDate(t *testing.T) {
	metricsbot.InitTelegramMetrics()
	r, w, c, _, b := durableVoiceFixture(t)
	a := durableVoiceApp(r, w, c)
	core := &confirmationTaskCore{durableVoiceCore: c}
	a.Core = core
	j := confirmationJob(t, uuid.NewString(), "smart", "A raw", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentTask, Confidence: 0.99, Title: "A task"}})
	j.ActiveDate = "01-Oct-2026"
	r.jobs[j.ID] = cloneDurableVoice(j)
	a.State.(*noteFlowState).value = tgstates.UserContext{UserID: 42, ActiveDate: "01-Jan-2030", SmartDraft: tgstates.SmartDraft{RawText: "B raw", TaskTitle: "B task"}, ReminderDraft: tgstates.ReminderDraft{Title: "B reminder"}}
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "pt"))
	require.Equal(t, j.ActiveDate, core.date)
	require.Equal(t, "A task", core.title)
	require.Empty(t, a.State.(*noteFlowState).value.ReminderDraft.Title)
}

func TestVoiceConfirmationReminderRestoresPreparedSnapshotAfterOtherDraft(t *testing.T) {
	r, w, c, _, b := durableVoiceFixture(t)
	a := durableVoiceApp(r, w, c)
	n := &confirmationNotifications{}
	a.Notifications = n
	j := confirmationJob(t, uuid.NewString(), "reminder", "A reminder text", voicePrepared{Reminder: &clients.LLMReminderResult{Title: "A reminder", ScheduleType: "once", Date: "2026-10-14", Hour: 9}})
	r.jobs[j.ID] = cloneDurableVoice(j)
	a.State.(*noteFlowState).value = tgstates.UserContext{UserID: 42, ActiveDate: "01-Jan-2030", ReminderDraft: tgstates.ReminderDraft{Title: "B reminder", ScheduleType: "once", Date: "2030-01-02"}}
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "rc"))
	require.Equal(t, []string{"A reminder"}, n.titles)
	require.Equal(t, []string{"2026-10-14"}, n.dates)
	require.Equal(t, j.Prepared, r.jobs[j.ID].Prepared)
}

func TestVoiceConfirmationUnknownReminderPersistsOriginalTimeParseBeforePreview(t *testing.T) {
	r, w, c, api, b := durableVoiceFixture(t)
	a := durableVoiceApp(r, w, c)
	store := &confirmationPreparedRepo{durableVoiceRepo: r}
	a.VoiceJobs = store
	llm := &durableVoiceLLM{}
	a.LLM = llm
	j := confirmationJob(t, uuid.NewString(), "smart", "завтра в 9 напомнить A", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentUnknown, Confidence: 0.3}})
	j.SentAt = time.Date(2026, 9, 30, 23, 15, 0, 0, time.UTC)
	j.TimezoneOffset, j.DayStartHour = 3, 7
	r.jobs[j.ID], r.done[j.ID] = cloneDurableVoice(j), true
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "pr"))
	require.Equal(t, "2026-10-01 02:15", llm.current)
	require.Equal(t, []string{"2026-09-30", "2026-10-01", "2026-10-02"}, llm.calendar)
	require.Equal(t, 1, llm.parses)
	require.Equal(t, 1, store.sets)
	require.Contains(t, api.markups[len(api.markups)-1], "voice:do:"+j.ID+":rc")
	var persisted voicePrepared
	require.NoError(t, json.Unmarshal(r.jobs[j.ID].Prepared, &persisted))
	require.Equal(t, "2026-10-01", persisted.Reminder.Date)
	// A different draft and session expiry cannot change the already shown parse.
	a.State.(*noteFlowState).value = tgstates.UserContext{UserID: 42, ReminderDraft: tgstates.ReminderDraft{Title: "B"}}
	a.LLM = nil
	n := &confirmationNotifications{}
	a.Notifications = n
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "rc"))
	require.Equal(t, []string{"synthetic reminder"}, n.titles)
	require.Equal(t, []string{"2026-10-01"}, n.dates)
	require.Empty(t, r.events)
}

func TestVoiceConfirmationPreparationCASUsesWinnerAndNeverDisplaysUncommittedParse(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent winner", true: "database failure"}[fail], func(t *testing.T) {
			r, w, c, api, b := durableVoiceFixture(t)
			a := durableVoiceApp(r, w, c)
			store := &confirmationPreparedRepo{durableVoiceRepo: r}
			a.VoiceJobs, a.LLM = store, &durableVoiceLLM{}
			j := confirmationJob(t, uuid.NewString(), "smart", "A reminder text", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentUnknown}})
			r.jobs[j.ID], r.done[j.ID] = cloneDurableVoice(j), true
			if fail {
				store.setError = errors.New("synthetic persistence failure")
			} else {
				var err error
				store.winner, err = json.Marshal(voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentUnknown}, Reminder: &clients.LLMReminderResult{Title: "winner reminder", ScheduleType: "once", Date: "2026-10-20", Hour: 10}})
				require.NoError(t, err)
			}
			err := a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "pr")
			if fail {
				require.Error(t, err)
				require.Empty(t, api.texts)
				require.Equal(t, j.Prepared, r.jobs[j.ID].Prepared)
			} else {
				require.NoError(t, err)
				require.Contains(t, api.texts[len(api.texts)-1], "winner reminder")
				require.Equal(t, "winner reminder", a.State.(*noteFlowState).value.ReminderDraft.Title)
			}
		})
	}
}

func TestVoiceConfirmationRejectsForeignUserChatAndUnknownOpcode(t *testing.T) {
	r, w, c, api, b := durableVoiceFixture(t)
	a := durableVoiceApp(r, w, c)
	j := confirmationJob(t, uuid.NewString(), "smart", "private text", voicePrepared{Intent: &clients.LLMIntentResult{Intent: clients.IntentNote}})
	r.jobs[j.ID] = cloneDurableVoice(j)
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID+1, j.ID, "y"))
	wrongChat := confirmationQuery(j)
	wrongChat.Message = &telego.Message{Chat: telego.Chat{ID: 999}}
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, wrongChat, j.UserID, j.ID, "y"))
	require.NoError(t, a.handleDurableVoiceConfirmation(t.Context(), b, confirmationQuery(j), j.UserID, j.ID, "unexpected"))
	require.Empty(t, c.calls)
	require.Empty(t, api.methods)
}
