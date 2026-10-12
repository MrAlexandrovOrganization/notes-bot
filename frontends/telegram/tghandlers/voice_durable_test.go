package tghandlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/voicejobs"
	pb "notes-bot/proto/whisper"
)

// Clone every read/write, as PostgreSQL JSON payloads do. Retaining a pointer
// here would accidentally make an uncommitted field survive the restart tests.
type durableVoiceRepo struct {
	jobs         map[string]*voicejobs.Job
	done         map[string]bool
	saveError    error
	enqueueError error
	events       []string
}

func newDurableVoiceRepo() *durableVoiceRepo {
	return &durableVoiceRepo{jobs: map[string]*voicejobs.Job{}, done: map[string]bool{}}
}
func cloneDurableVoice(j *voicejobs.Job) *voicejobs.Job {
	if j == nil {
		return nil
	}
	copy := *j
	copy.Prepared = append(json.RawMessage(nil), j.Prepared...)
	return &copy
}
func (r *durableVoiceRepo) Enqueue(_ context.Context, j *voicejobs.Job) error {
	if r.enqueueError != nil {
		return r.enqueueError
	}
	for _, old := range r.jobs {
		if old.ChatID == j.ChatID && old.MessageID == j.MessageID {
			return nil
		}
	}
	r.jobs[j.ID] = cloneDurableVoice(j)
	return nil
}
func (r *durableVoiceRepo) Find(_ context.Context, chat int64, msg int) (*voicejobs.Job, error) {
	for _, j := range r.jobs {
		if j.ChatID == chat && j.MessageID == msg {
			return cloneDurableVoice(j), nil
		}
	}
	return nil, nil
}
func (r *durableVoiceRepo) Claim(context.Context) (*voicejobs.Job, error) {
	for id, j := range r.jobs {
		if !r.done[id] {
			return cloneDurableVoice(j), nil
		}
	}
	return nil, nil
}
func (r *durableVoiceRepo) Save(_ context.Context, j *voicejobs.Job) error {
	if r.saveError != nil {
		return r.saveError
	}
	r.events = append(r.events, "save")
	copy := cloneDurableVoice(j)
	if old := r.jobs[j.ID]; old != nil && old.CancelRequested {
		copy.CancelRequested = true
	}
	r.jobs[j.ID] = copy
	return nil
}
func (r *durableVoiceRepo) Release(ctx context.Context, j *voicejobs.Job, done bool, _ time.Duration) error {
	if err := r.Save(ctx, j); err != nil {
		return err
	}
	r.done[j.ID] = done
	return nil
}
func (r *durableVoiceRepo) IsFront(_ context.Context, j *voicejobs.Job) (bool, error) {
	for id, older := range r.jobs {
		if older.ChatID == j.ChatID && older.MessageID < j.MessageID && !r.done[id] {
			return false, nil
		}
	}
	return true, nil
}
func (r *durableVoiceRepo) Cancel(_ context.Context, id string, user int64) error {
	if j := r.jobs[id]; j != nil && j.UserID == user && !r.done[id] {
		j.CancelRequested = true
	}
	return nil
}
func (r *durableVoiceRepo) Get(_ context.Context, id string, user int64) (*voicejobs.Job, error) {
	j := r.jobs[id]
	if j == nil || j.UserID != user {
		return nil, nil
	}
	return cloneDurableVoice(j), nil
}
func (r *durableVoiceRepo) Page(_ context.Context, chat, user int64, msg int) (*voicejobs.Job, error) {
	for _, j := range r.jobs {
		if j.ChatID == chat && j.UserID == user && j.StatusMessageID == msg {
			return cloneDurableVoice(j), nil
		}
	}
	return nil, nil
}

type durableVoiceWhisper struct {
	clients.WhisperService
	repo           *durableVoiceRepo
	requests       []string
	remote         map[string]string
	ackRequests    []string
	cancelRequests []string
	loseSubmit     bool
	loseAck        bool
	ackMissing     bool
	statusError    error
	result         *clients.JobResult
	ackBeforeSaved bool
}

func (w *durableVoiceWhisper) SubmitOnce(_ context.Context, r io.Reader, format, preset, key string) (string, int, error) {
	if _, err := io.ReadAll(r); err != nil {
		return "", 0, err
	}
	w.requests = append(w.requests, key)
	if w.remote == nil {
		w.remote = map[string]string{}
	}
	if w.remote[key] == "" {
		w.remote[key] = fmt.Sprintf("remote-%d", len(w.remote)+1)
	}
	if w.loseSubmit {
		w.loseSubmit = false
		return "", 0, status.Error(codes.Unavailable, "synthetic lost submit reply")
	}
	return w.remote[key], 1, nil
}
func (w *durableVoiceWhisper) GetStatus(context.Context, string) (*clients.JobResult, error) {
	if w.statusError != nil {
		return nil, w.statusError
	}
	return w.result, nil
}
func (w *durableVoiceWhisper) Ack(_ context.Context, id string) (bool, error) {
	w.ackRequests = append(w.ackRequests, id)
	w.repo.events = append(w.repo.events, "ack")
	found := false
	for _, j := range w.repo.jobs {
		if j.RemoteID == id && j.ResultReady {
			found = true
		}
	}
	if !found {
		w.ackBeforeSaved = true
	}
	if w.loseAck {
		w.loseAck = false
		return false, status.Error(codes.Unavailable, "synthetic lost ack reply")
	}
	if w.ackMissing {
		return false, status.Error(codes.NotFound, "acknowledged result already expired")
	}
	return true, nil
}
func (w *durableVoiceWhisper) Cancel(_ context.Context, id string) (bool, error) {
	w.cancelRequests = append(w.cancelRequests, id)
	return true, nil
}

type durableVoiceCore struct {
	clients.CoreService
	operations map[string]string
	calls      []string
	loseReply  bool
}

func (c *durableVoiceCore) AppendToNoteOnce(_ context.Context, date, text, key string) (bool, error) {
	c.calls = append(c.calls, key)
	if c.operations == nil {
		c.operations = map[string]string{}
	}
	payload := date + "\x00" + text
	if previous, ok := c.operations[key]; ok && previous != payload {
		return false, errors.New("operation payload conflict")
	}
	c.operations[key] = payload
	if c.loseReply {
		c.loseReply = false
		return false, status.Error(codes.Unavailable, "synthetic lost note reply")
	}
	return true, nil
}

type durableVoiceAPI struct {
	audio       string
	methods     []string
	texts       []string
	markups     []string
	messageID   int
	missingEdit bool
	failMenu    bool
	failStatus  bool
	failEdit    bool
	lastEditID  int
}

func (api *durableVoiceAPI) call(req *http.Request) (*http.Response, error) {
	method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
	api.methods = append(api.methods, method)
	var params map[string]json.RawMessage
	if req.Body != nil {
		if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
			return nil, err
		}
	}
	if value := params["text"]; len(value) > 0 {
		var text string
		_ = json.Unmarshal(value, &text)
		api.texts = append(api.texts, text)
	}
	api.markups = append(api.markups, string(params["reply_markup"]))
	if method == "editMessageText" {
		_ = json.Unmarshal(params["message_id"], &api.lastEditID)
		if api.failEdit {
			api.failEdit = false
			return apiResponse(`{"ok":false,"error_code":503,"description":"synthetic Telegram unavailable"}`), nil
		}
	}
	if method == "sendMessage" && api.failStatus {
		api.failStatus = false
		return apiResponse(`{"ok":false,"error_code":503,"description":"synthetic Telegram unavailable"}`), nil
	}
	if method == "sendMessage" && api.failMenu && strings.Contains(string(params["reply_markup"]), "menu:note") {
		api.failMenu = false
		return apiResponse(`{"ok":false,"error_code":503,"description":"synthetic Telegram unavailable"}`), nil
	}
	if method == "editMessageText" && api.missingEdit {
		api.missingEdit = false
		return apiResponse(`{"ok":false,"error_code":400,"description":"Bad Request: message to edit not found"}`), nil
	}
	if method == "getFile" {
		data, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{"file_id": "synthetic", "file_path": api.audio}})
		return apiResponse(string(data)), nil
	}
	api.messageID++
	return apiResponse(fmt.Sprintf(`{"ok":true,"result":{"message_id":%d,"date":1,"chat":{"id":42,"type":"private"}}}`, api.messageID)), nil
}
func durableVoiceApp(r *durableVoiceRepo, w *durableVoiceWhisper, c *durableVoiceCore) *App {
	return &App{Cfg: &config.Config{RootID: 42, TimezoneOffsetHours: 3, DayStartHour: 7, LocalAPIURL: "http://synthetic-local"},
		State:     &noteFlowState{value: tgstates.UserContext{UserID: 42, State: tgstates.StateIdle, ActiveDate: "12-Oct-2026"}},
		VoiceJobs: r, Whisper: w, Core: c, Logger: zap.NewNop()}
}
func durableVoiceFixture(t *testing.T) (*durableVoiceRepo, *durableVoiceWhisper, *durableVoiceCore, *durableVoiceAPI, *telego.Bot) {
	t.Helper()
	r := newDurableVoiceRepo()
	w := &durableVoiceWhisper{repo: r, result: &clients.JobResult{Status: pb.JobStatus_DONE, Text: "synthetic <voice> & result"}}
	c := &durableVoiceCore{}
	path := filepath.Join(t.TempDir(), "synthetic.ogg")
	require.NoError(t, os.WriteFile(path, []byte("synthetic audio"), 0600))
	api := &durableVoiceAPI{audio: path, messageID: 70}
	bot := fakeTelego(t, api.call)
	return r, w, c, api, bot
}
func durableVoiceJob(id string, msg int) *voicejobs.Job {
	return &voicejobs.Job{ID: id, ChatID: 42, UserID: 42, MessageID: msg, StatusMessageID: msg + 100, FileID: "synthetic-file", Format: "ogg", ActiveDate: "12-Oct-2026", Mode: "note", SentAt: time.Now().Add(-30 * 24 * time.Hour)}
}
func durableVoiceStep(t *testing.T, r *durableVoiceRepo, w *durableVoiceWhisper, c *durableVoiceCore, b *telego.Bot, id string) (bool, time.Duration, error) {
	t.Helper()
	j, err := r.Get(t.Context(), id, 42)
	require.NoError(t, err)
	require.NotNil(t, j)
	// Reconstruct App on every step: no in-memory caches survive between calls.
	done, delay, err := durableVoiceApp(r, w, c).voiceStep(t.Context(), b, j)
	if err == nil {
		require.NoError(t, r.Release(t.Context(), j, done, delay))
	}
	return done, delay, err
}

func TestDurableVoiceAcceptSnapshotsDeduplicatesAndPersistsErrors(t *testing.T) {
	for _, mode := range []struct {
		state tgstates.UserState
		want  string
	}{{tgstates.StateIdle, "note"}, {tgstates.StateReminderCreateNL, "reminder"}, {tgstates.StateSmartInput, "smart"}} {
		t.Run(mode.want, func(t *testing.T) {
			r, w, c, _, bot := durableVoiceFixture(t)
			app := durableVoiceApp(r, w, c)
			state := app.State.(*noteFlowState)
			state.value.State = mode.state
			update := &telego.Update{Message: &telego.Message{MessageID: 7, Date: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC).Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "original-audio"}}}
			require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
			first, err := r.Find(t.Context(), 42, 7)
			require.NoError(t, err)
			assert.Equal(t, "12-Oct-2026", first.ActiveDate)
			assert.Equal(t, mode.want, first.Mode)
			assert.Equal(t, "original-audio", first.FileID)
			assert.Equal(t, 3, first.TimezoneOffset)
			assert.Equal(t, 7, first.DayStartHour)
			state.value.ActiveDate = "01-Jan-2030"
			state.value.State = tgstates.StateIdle
			update.Message.Voice.FileID = "duplicate-different-audio"
			require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
			second, err := r.Find(t.Context(), 42, 7)
			require.NoError(t, err)
			assert.Equal(t, first, second)
			assert.Len(t, r.jobs, 1)
			update.Message.MessageID = 8
			r.enqueueError = errors.New("synthetic database unavailable")
			assert.Error(t, app.AcceptVoice(t.Context(), bot, update))
			assert.Len(t, r.jobs, 1)
		})
	}
}

func TestDurableVoiceSendsMenuBeforeTranscription(t *testing.T) {
	for _, state := range []tgstates.UserState{tgstates.StateIdle, tgstates.StateSmartInput, tgstates.StateReminderCreateNL} {
		for _, video := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/video=%t", state, video), func(t *testing.T) {
				r, w, c, api, bot := durableVoiceFixture(t)
				app := durableVoiceApp(r, w, c)
				app.State.(*noteFlowState).value.State = state
				message := &telego.Message{MessageID: 7, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}}
				if video {
					message.VideoNote = &telego.VideoNote{FileID: "synthetic-video"}
				} else {
					message.Voice = &telego.Voice{FileID: "synthetic-voice"}
				}
				require.NoError(t, app.AcceptVoice(t.Context(), bot, &telego.Update{Message: message}))
				job, err := r.Find(t.Context(), 42, 7)
				require.NoError(t, err)
				require.NotNil(t, job)
				// The handler sends both messages without starting the worker.
				assert.Equal(t, []string{"sendMessage", "sendMessage"}, api.methods)
				assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceContinue}, api.texts)
				assert.Equal(t, 71, job.StatusMessageID)
				assert.Contains(t, api.markups[0], "voice:stop:"+job.ID)
				var menu telego.InlineKeyboardMarkup
				require.NoError(t, json.Unmarshal([]byte(api.markups[1]), &menu))
				assert.Equal(t, tgkeyboards.MainMenu(""), menu)
				assert.Empty(t, w.requests)
				assert.Empty(t, c.calls)
				assert.Equal(t, state, app.State.(*noteFlowState).value.State)

				// Duplicate updates and resumed worker steps must not send
				// another status destination or another menu.
				require.NoError(t, app.AcceptVoice(t.Context(), bot, &telego.Update{Message: message}))
				for range 2 {
					_, _, err = durableVoiceStep(t, r, w, c, bot, job.ID)
					require.NoError(t, err)
				}
				assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceContinue, resources.VoiceDownloading, resources.VoiceSubmitting}, api.texts)
				assert.Equal(t, job.StatusMessageID, r.jobs[job.ID].StatusMessageID)
				if state == tgstates.StateIdle {
					for range 4 {
						_, _, err = durableVoiceStep(t, r, w, c, bot, job.ID)
						require.NoError(t, err)
					}
					assert.True(t, r.done[job.ID])
					assert.Equal(t, job.StatusMessageID, api.lastEditID)
					assert.Equal(t, []string{"sendMessage", "sendMessage", "editMessageText", "getFile", "editMessageText", "editMessageText"}, api.methods)
					assert.Contains(t, api.texts[len(api.texts)-1], resources.VoiceSaved)
				}
			})
		}
	}
}

func TestDurableVoiceMenuFailureDoesNotRejectOrBlockAcceptedJob(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	app := durableVoiceApp(r, w, c)
	api.failMenu = true
	update := &telego.Update{Message: &telego.Message{MessageID: 7, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "synthetic-voice"}}}
	require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
	job, err := r.Find(t.Context(), 42, 7)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, 71, job.StatusMessageID)
	assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceContinue}, api.texts)

	require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
	for range 2 {
		_, _, err = durableVoiceStep(t, r, w, c, bot, job.ID)
		require.NoError(t, err)
	}
	assert.Len(t, w.requests, 1)
	assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceContinue, resources.VoiceDownloading, resources.VoiceSubmitting}, api.texts)
}

func TestDurableVoiceProgressUpdatesStatusAndSurvivesRestart(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	j := durableVoiceJob("progress", 1)
	j.RemoteID = "remote-progress"
	require.NoError(t, r.Enqueue(t.Context(), j))
	for _, update := range []struct {
		status  pb.JobStatus
		percent float32
		want    string
	}{
		{pb.JobStatus_ACCEPTED, 0, resources.VoiceQueued},
		{pb.JobStatus_QUEUED, 0, resources.VoiceQueued},
		{pb.JobStatus_DOWNLOADING, 0, resources.VoiceUploading},
		{pb.JobStatus_RUNNING, 0, resources.VoiceRunning},
		{pb.JobStatus_RUNNING, 27.6, "⏳ Расшифровываю... 28%"},
		{pb.JobStatus_RUNNING, 27.7, "⏳ Расшифровываю... 28%"},
		{pb.JobStatus_RUNNING, 54.2, "⏳ Расшифровываю... 54%"},
		{pb.JobStatus_RUNNING, 120, "⏳ Расшифровываю... 100%"},
		{pb.JobStatus_RUNNING, -1, resources.VoiceRunning},
		{pb.JobStatus_RUNNING, float32(math.NaN()), resources.VoiceRunning},
		{pb.JobStatus_RUNNING, float32(math.Inf(1)), resources.VoiceRunning},
	} {
		w.result = &clients.JobResult{Status: update.status, ProgressPercent: update.percent}
		previous, edits := r.jobs[j.ID].LastStatus, len(api.texts)
		// The helper rebuilds App from the persisted job for every poll.
		done, delay, err := durableVoiceStep(t, r, w, c, bot, j.ID)
		require.NoError(t, err)
		assert.False(t, done)
		assert.Equal(t, 5*time.Second, delay)
		assert.Equal(t, update.want, r.jobs[j.ID].LastStatus)
		if update.want == previous {
			assert.Len(t, api.texts, edits)
		} else {
			require.Len(t, api.texts, edits+1)
			assert.Equal(t, update.want, api.texts[edits])
			assert.Equal(t, j.StatusMessageID, api.lastEditID)
			assert.Contains(t, api.markups[len(api.markups)-1], "voice:stop:"+j.ID)
		}
	}
	assert.Empty(t, w.requests)
	assert.Empty(t, c.calls)
}

func TestDurableVoiceProgressFailureDoesNotBlockUploadAndRetriesPollUpdate(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	j := durableVoiceJob("progress-retry", 1)
	j.SubmitStarted = true
	require.NoError(t, r.Enqueue(t.Context(), j))
	api.failEdit = true
	_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.Len(t, w.requests, 1)
	assert.NotEmpty(t, r.jobs[j.ID].RemoteID)
	assert.Equal(t, resources.VoiceSubmitting, r.jobs[j.ID].LastStatus)

	w.result = &clients.JobResult{Status: pb.JobStatus_RUNNING, ProgressPercent: 42}
	api.failEdit = true
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.Equal(t, resources.VoiceSubmitting, r.jobs[j.ID].LastStatus)
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.Equal(t, "⏳ Расшифровываю... 42%", r.jobs[j.ID].LastStatus)
	assert.Equal(t, []string{resources.VoiceDownloading, resources.VoiceSubmitting, "⏳ Расшифровываю... 42%", "⏳ Расшифровываю... 42%"}, api.texts)
}

func TestDurableVoiceEnqueueFailureDoesNotShowSuccessMenu(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	app := durableVoiceApp(r, w, c)
	r.enqueueError = errors.New("synthetic database unavailable")
	update := &telego.Update{Message: &telego.Message{MessageID: 7, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "synthetic-voice"}}}
	require.Error(t, app.AcceptVoice(t.Context(), bot, update))
	assert.Empty(t, r.jobs)
	assert.Equal(t, []string{"sendMessage", "editMessageText"}, api.methods)
	assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceAcceptanceRetry}, api.texts)
	assert.Empty(t, w.requests)

	r.enqueueError = nil
	require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
	assert.Len(t, r.jobs, 1)
	assert.Equal(t, resources.VoiceContinue, api.texts[len(api.texts)-1])
}

func TestDurableVoiceStatusFailureRetriesBeforePublishingJob(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	app := durableVoiceApp(r, w, c)
	api.failStatus = true
	update := &telego.Update{Message: &telego.Message{MessageID: 7, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "synthetic-voice"}}}
	require.Error(t, app.AcceptVoice(t.Context(), bot, update))
	assert.Empty(t, r.jobs)
	assert.Equal(t, []string{"sendMessage"}, api.methods)
	assert.Empty(t, w.requests)

	require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
	assert.Len(t, r.jobs, 1)
	assert.Equal(t, []string{resources.VoiceReceiving, resources.VoiceReceiving, resources.VoiceContinue}, api.texts)
}

func TestDurableVoiceSubmitResultAckAndNoteSurviveLostReplies(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	j := durableVoiceJob("stable-one", 1)
	require.NoError(t, r.Enqueue(t.Context(), j))
	_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.True(t, r.jobs[j.ID].SubmitStarted)
	assert.Empty(t, w.requests)
	w.loseSubmit = true
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.Error(t, err)
	assert.Empty(t, r.jobs[j.ID].RemoteID)
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.Len(t, w.remote, 1)
	assert.Equal(t, []string{"notes-bot:stable-one", "notes-bot:stable-one"}, w.requests)
	r.saveError = errors.New("synthetic result checkpoint failure")
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.Error(t, err)
	assert.False(t, r.jobs[j.ID].ResultReady)
	assert.Empty(t, w.ackRequests)
	r.saveError = nil
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.True(t, r.jobs[j.ID].ResultReady)
	assert.Equal(t, w.result.Text, r.jobs[j.ID].Text)
	assert.Empty(t, w.ackRequests)
	w.loseAck = true
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.Error(t, err)
	assert.False(t, r.jobs[j.ID].Acked)
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.True(t, r.jobs[j.ID].Acked)
	assert.False(t, w.ackBeforeSaved)
	assert.Equal(t, []string{r.jobs[j.ID].RemoteID, r.jobs[j.ID].RemoteID}, w.ackRequests)
	c.loseReply = true
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.Error(t, err)
	assert.False(t, r.jobs[j.ID].NoteSaved)
	_, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.True(t, r.jobs[j.ID].NoteSaved)
	assert.Len(t, c.operations, 1)
	assert.Equal(t, []string{"voice:stable-one", "voice:stable-one"}, c.calls)
	done, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Contains(t, api.texts[len(api.texts)-1], "&lt;voice&gt; &amp; result")
	assert.Equal(t, "save", r.events[0])
}

func TestDurableVoiceLongWorkerAbsenceKeepsJobAndOriginalDate(t *testing.T) {
	r, w, c, _, bot := durableVoiceFixture(t)
	j := durableVoiceJob("offline", 1)
	j.RemoteID = "remote-offline"
	j.SubmitStarted = true
	require.NoError(t, r.Enqueue(t.Context(), j))
	w.statusError = status.Error(codes.Unavailable, "synthetic unavailable")
	for range 100 {
		done, delay, err := durableVoiceStep(t, r, w, c, bot, j.ID)
		assert.Error(t, err)
		assert.False(t, done)
		assert.Positive(t, delay)
		assert.False(t, r.done[j.ID])
	}
	w.statusError = nil
	w.result = &clients.JobResult{Status: pb.JobStatus_QUEUED}
	done, delay, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Positive(t, delay)
	w.result = &clients.JobResult{Status: pb.JobStatus_DONE, Text: "late result"}
	for range 4 {
		_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
		require.NoError(t, err)
	}
	assert.True(t, r.done[j.ID])
	assert.Equal(t, "12-Oct-2026\x00late result", c.operations["voice:offline"])
}

func TestDurableVoiceAckExpiredAfterLostReplyStillDelivers(t *testing.T) {
	r, w, c, _, bot := durableVoiceFixture(t)
	j := durableVoiceJob("ack-expired", 1)
	j.RemoteID = "expired"
	j.ResultReady = true
	j.Text = "durable local result"
	require.NoError(t, r.Enqueue(t.Context(), j))
	w.ackMissing = true
	for range 3 {
		_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
		require.NoError(t, err)
	}
	assert.True(t, r.done[j.ID])
	assert.Len(t, c.operations, 1)
}

func TestDurableVoiceCancellationResolvesLostSubmitAndSurvivesRestart(t *testing.T) {
	r, w, c, _, bot := durableVoiceFixture(t)
	j := durableVoiceJob("cancel-lost-submit", 1)
	j.SubmitStarted = true
	require.NoError(t, r.Enqueue(t.Context(), j))
	w.loseSubmit = true
	_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.Error(t, err)
	query := &telego.CallbackQuery{Message: &telego.Message{MessageID: j.StatusMessageID, Chat: telego.Chat{ID: 42}}}
	require.NoError(t, durableVoiceApp(r, w, c).handleVoiceAction(t.Context(), bot, query, 42, []string{"voice", "stop", j.ID}))
	for range 4 {
		_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
		require.NoError(t, err)
	}
	assert.True(t, r.done[j.ID])
	assert.Len(t, w.remote, 1)
	assert.Len(t, w.cancelRequests, 1)
	assert.Len(t, w.ackRequests, 1)
	assert.Empty(t, c.operations)
	assert.Equal(t, "cancelled", r.jobs[j.ID].Error)
}

func TestDurableVoiceRestartPreservesOrderingPaginationAndOwnership(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	earlier := durableVoiceJob("earlier", 1)
	later := durableVoiceJob("later", 2)
	for _, j := range []*voicejobs.Job{earlier, later} {
		j.ResultReady = true
		j.Acked = true
		j.Text = strings.Repeat("Ж", voiceCharsPerPage) + "<last-page>"
		require.NoError(t, r.Enqueue(t.Context(), j))
	}
	done, _, err := durableVoiceStep(t, r, w, c, bot, later.ID)
	require.NoError(t, err)
	assert.False(t, done)
	assert.Empty(t, c.calls)
	for _, j := range []*voicejobs.Job{earlier, later} {
		for range 2 {
			_, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
			require.NoError(t, err)
		}
	}
	assert.Equal(t, []string{"voice:earlier", "voice:later"}, c.calls)
	query := &telego.CallbackQuery{Message: &telego.Message{MessageID: later.StatusMessageID, Chat: telego.Chat{ID: 42}}}
	parts := []string{"voice", "pages", later.ID, "1"}
	require.NoError(t, durableVoiceApp(r, w, c).handleVoiceAction(t.Context(), bot, query, 42, parts))
	assert.Contains(t, api.texts[len(api.texts)-1], "[2/2]")
	assert.Contains(t, api.texts[len(api.texts)-1], "&lt;last-page&gt;")
	count := len(api.texts)
	require.NoError(t, durableVoiceApp(r, w, c).handleVoiceAction(t.Context(), bot, query, 99, parts))
	assert.Len(t, api.texts, count)
}

func TestDurableVoiceRecreatedResultKeepsPaginationAfterRestart(t *testing.T) {
	r, w, c, api, bot := durableVoiceFixture(t)
	j := durableVoiceJob("deleted-result", 1)
	j.ResultReady, j.Acked, j.NoteSaved = true, true, true
	j.Text = strings.Repeat("Ж", voiceCharsPerPage) + "<last-page>"
	require.NoError(t, r.Enqueue(t.Context(), j))
	api.missingEdit = true
	done, _, err := durableVoiceStep(t, r, w, c, bot, j.ID)
	require.NoError(t, err)
	require.True(t, done)
	stored := r.jobs[j.ID]
	require.NotEqual(t, j.StatusMessageID, stored.StatusMessageID)
	assert.Contains(t, api.markups[len(api.markups)-1], fmt.Sprintf("voice:pages:%s:1", stored.ID))
	query := &telego.CallbackQuery{Message: &telego.Message{MessageID: stored.StatusMessageID, Chat: telego.Chat{ID: 42}}}
	require.NoError(t, durableVoiceApp(r, w, c).handleVoiceAction(t.Context(), bot, query, 42, []string{"voice", "pages", stored.ID, "1"}))
	assert.Contains(t, api.texts[len(api.texts)-1], "&lt;last-page&gt;")
}

type durableVoiceLLM struct {
	clients.LLMService
	current         string
	calendar        []string
	classifications int
	parses          int
}

func (l *durableVoiceLLM) ClassifyIntent(_ context.Context, _ string, current string) (*clients.LLMIntentResult, error) {
	l.current = current
	l.classifications++
	return &clients.LLMIntentResult{Intent: clients.IntentReminder, Confidence: 0.99, Title: "synthetic reminder"}, nil
}
func (l *durableVoiceLLM) ParseReminder(_ context.Context, _ string, current, today, tomorrow, after string) (*clients.LLMReminderResult, error) {
	l.current = current
	l.calendar = []string{today, tomorrow, after}
	l.parses++
	return &clients.LLMReminderResult{Title: "synthetic reminder", ScheduleType: "once", Date: tomorrow, Hour: 9, Minute: 15}, nil
}

func TestDurableVoicePreparedIntentUsesOriginalTimestampAndRestoresExpiredDraft(t *testing.T) {
	for _, mode := range []string{"smart", "reminder"} {
		t.Run(mode, func(t *testing.T) {
			r, w, c, api, bot := durableVoiceFixture(t)
			j := durableVoiceJob("prepared-"+mode, 1)
			j.Mode = mode
			j.SentAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			j.TimezoneOffset = 3
			j.DayStartHour = 7
			j.ResultReady = true
			j.Acked = true
			j.Text = "tomorrow at nine"
			j.RemoteID = "remote-prepared"
			require.NoError(t, r.Enqueue(t.Context(), j))
			app := durableVoiceApp(r, w, c)
			llm := &durableVoiceLLM{}
			app.LLM = llm
			copy, err := r.Get(t.Context(), j.ID, 42)
			require.NoError(t, err)
			done, delay, err := app.voiceStep(t.Context(), bot, copy)
			require.NoError(t, err)
			assert.False(t, done)
			require.NoError(t, r.Release(t.Context(), copy, done, delay))
			assert.Equal(t, "2026-10-01 03:00", llm.current)
			assert.Equal(t, []string{"2026-09-30", "2026-10-01", "2026-10-02"}, llm.calendar)
			assert.Equal(t, 1, llm.parses)
			require.NotEmpty(t, r.jobs[j.ID].Prepared)
			assert.Empty(t, c.operations)
			// Restart with no LLM dependency: the persisted preparation suffices.
			done, _, err = durableVoiceStep(t, r, w, c, bot, j.ID)
			require.NoError(t, err)
			assert.True(t, done)
			assert.Contains(t, api.markups[len(api.markups)-1], "voice:open:"+j.ID)
			for range 2 {
				restarted := durableVoiceApp(r, w, c)
				state := restarted.State.(*noteFlowState)
				state.value = tgstates.UserContext{UserID: 42, State: tgstates.StateIdle, ActiveDate: "01-Jan-2030"}
				query := &telego.CallbackQuery{Message: &telego.Message{MessageID: j.StatusMessageID, Chat: telego.Chat{ID: 42}}}
				require.NoError(t, restarted.handleVoiceAction(t.Context(), bot, query, 42, []string{"voice", "open", j.ID}))
				assert.Equal(t, j.ActiveDate, state.value.ActiveDate)
				assert.Equal(t, "2026-10-01", state.value.ReminderDraft.Date)
				assert.Equal(t, "synthetic reminder", state.value.ReminderDraft.Title)
				if mode == "smart" {
					assert.Equal(t, tgstates.StateSmartConfirm, state.value.State)
					assert.Equal(t, j.Text, state.value.SmartDraft.RawText)
				} else {
					assert.Equal(t, tgstates.StateReminderCreateTaskConfirm, state.value.State)
				}
				assert.Contains(t, api.markups[len(api.markups)-1], "voice:open:"+j.ID)
			}
			assert.Equal(t, 1, llm.parses)
			assert.Empty(t, c.operations)
		})
	}
}
