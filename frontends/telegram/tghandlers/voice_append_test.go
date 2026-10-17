package tghandlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/voicejobs"
)

type voicePathCore struct {
	*durableVoiceCore
	operations map[string]string
	pathCalls  []string
	loseReply  bool
	fail       bool
}

func (c *voicePathCore) AppendToNoteByPathOnce(_ context.Context, path, text, id string) (bool, error) {
	c.pathCalls = append(c.pathCalls, path)
	if c.fail {
		return false, errors.New("synthetic missing selected note")
	}
	if c.operations == nil {
		c.operations = map[string]string{}
	}
	payload := path + "\x00" + text
	if old, ok := c.operations[id]; ok && old != payload {
		return false, errors.New("synthetic operation conflict")
	}
	c.operations[id] = payload
	if c.loseReply {
		c.loseReply = false
		return false, errors.New("synthetic lost reply after append")
	}
	return true, nil
}

func (c *voicePathCore) AppendToNoteByPath(ctx context.Context, path, text string) (bool, error) {
	return c.AppendToNoteByPathOnce(ctx, path, text, "legacy")
}

func TestVoiceAppendKeepsSelectedNoteAfterNavigationRestartAndLostReply(t *testing.T) {
	for _, source := range []tgstates.UserState{tgstates.StateBrowseFile, tgstates.StateViewNote} {
		for _, video := range []bool{false, true} {
			name := string(source) + "/voice"
			if video {
				name = string(source) + "/video"
			}
			t.Run(name, func(t *testing.T) {
				r, w, daily, api, bot := durableVoiceFixture(t)
				core := &voicePathCore{durableVoiceCore: daily, loseReply: true}
				app := durableVoiceApp(r, w, daily)
				app.Core = core
				state := app.State.(*noteFlowState)
				const original = "Tags/Фильмы/Первый <фильм>.md"
				state.value.State, state.value.ActiveRelpath = source, original
				query := &telego.CallbackQuery{Message: &telego.Message{MessageID: 1, Chat: telego.Chat{ID: 42}}}
				require.NoError(t, app.handleNoteAppendAction(t.Context(), bot, query, 42))
				require.Equal(t, tgstates.StateAppendToNoteInput, state.value.State)
				message := &telego.Message{MessageID: 7, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}}
				if video {
					message.VideoNote = &telego.VideoNote{FileID: "synthetic-video"}
				} else {
					message.Voice = &telego.Voice{FileID: "synthetic-voice"}
				}
				update := &telego.Update{Message: message}
				require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
				job, err := r.Find(t.Context(), 42, 7)
				require.NoError(t, err)
				require.Equal(t, original, job.ActiveRelpath)
				state.value.ActiveRelpath = "Tags/Фильмы/Другой.md"
				state.value.ActiveDate = "01-Jan-2030"
				state.value.State = tgstates.StateBrowseFile
				// A duplicate Telegram update must keep the first destination.
				require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
				replayed, err := r.Find(t.Context(), 42, 7)
				require.NoError(t, err)
				require.Equal(t, job, replayed)
				job.Text, job.ResultReady, job.Acked = "Synthetic film review", true, true
				// Round-trip the payload as PostgreSQL does, then recreate App on
				// every attempt to exercise delivery after a frontend restart.
				encoded, err := json.Marshal(job)
				require.NoError(t, err)
				var restored voicejobs.Job
				require.NoError(t, json.Unmarshal(encoded, &restored))
				r.jobs[job.ID] = &restored
				resume := func() (bool, error) {
					a := durableVoiceApp(r, w, daily)
					a.Core, a.State = core, state
					pending, err := r.Get(t.Context(), job.ID, 42)
					require.NoError(t, err)
					done, delay, err := a.voiceStep(t.Context(), bot, pending)
					if err == nil {
						require.NoError(t, r.Release(t.Context(), pending, done, delay))
					}
					return done, err
				}
				_, err = resume()
				require.Error(t, err, "the first reply is lost after the write")
				require.False(t, r.jobs[job.ID].NoteSaved)
				_, err = resume()
				require.NoError(t, err)
				done, err := resume()
				require.NoError(t, err)
				require.True(t, done)
				require.Equal(t, []string{original, original}, core.pathCalls)
				require.Len(t, core.operations, 1)
				require.Equal(t, original+"\x00"+job.Text, core.operations["voice:"+job.ID])
				require.Empty(t, daily.calls, "the daily note must not receive the text")
				require.Equal(t, "Tags/Фильмы/Другой.md", state.value.ActiveRelpath)
				require.Equal(t, tgstates.StateBrowseFile, state.value.State)
				require.Contains(t, api.texts[len(api.texts)-1], "Первый &lt;фильм&gt;.md")
			})
		}
	}
}

func TestVoiceAppendNeverFallsBackToDaily(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		r, w, daily, _, bot := durableVoiceFixture(t)
		app := durableVoiceApp(r, w, daily)
		if !unsupported {
			app.Core = &voicePathCore{durableVoiceCore: daily, fail: true}
		}
		job := durableVoiceJob("missing-target", 7)
		job.ActiveRelpath, job.Text, job.ResultReady, job.Acked = "Films/Deleted.md", "review", true, true
		_, _, err := app.voiceStep(t.Context(), bot, job)
		require.Error(t, err)
		require.False(t, job.NoteSaved)
		require.Empty(t, daily.calls)
	}
}

func TestVoiceAppendRejectsMissingDestinationAtAcceptance(t *testing.T) {
	r, w, daily, api, bot := durableVoiceFixture(t)
	app := durableVoiceApp(r, w, daily)
	app.State.(*noteFlowState).value.State = tgstates.StateAppendToNoteInput
	message := &telego.Message{MessageID: 7, From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "synthetic"}}
	require.NoError(t, app.AcceptVoice(t.Context(), bot, &telego.Update{Message: message}))
	require.Empty(t, r.jobs)
	require.Equal(t, []string{resources.VoiceMissingDestination}, api.texts)
}

func TestLegacyVoiceDeliveryUsesCapturedNotePath(t *testing.T) {
	r, w, daily, _, bot := durableVoiceFixture(t)
	app := durableVoiceApp(r, w, daily)
	core := &voicePathCore{durableVoiceCore: daily}
	app.Core = core
	app.State.(*noteFlowState).value.ActiveRelpath = "Films/Other.md"
	app.deliverVoiceResult(&pendingVoiceResult{tgBot: bot, chatID: 42, userID: 42, statusMsgID: 1,
		activeDate: "12-Oct-2026", activeRelpath: "Films/Original.md", text: "review", log: zap.NewNop()})
	require.Equal(t, []string{"Films/Original.md"}, core.pathCalls)
	require.Empty(t, daily.calls)
}
