package tghandlers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"notes-bot/core"
	"notes-bot/frontends/telegram/clients"
	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/voicejobs"
	pb "notes-bot/proto/english/v1"
)

type englishFlowClient struct {
	pb.EnglishServiceClient
	server              *core.EnglishServer
	saves, grades       int
	loseSave, loseGrade bool
}

func (c *englishFlowClient) SaveCard(ctx context.Context, req *pb.SaveCardRequest, _ ...grpc.CallOption) (*pb.SaveCardResponse, error) {
	c.saves++
	result, err := c.server.SaveCard(ctx, req)
	if err == nil && c.loseSave {
		c.loseSave = false
		return nil, errors.New("lost save reply")
	}
	return result, err
}
func (c *englishFlowClient) ListCards(ctx context.Context, req *pb.ListCardsRequest, _ ...grpc.CallOption) (*pb.ListCardsResponse, error) {
	return c.server.ListCards(ctx, req)
}
func (c *englishFlowClient) GradeCard(ctx context.Context, req *pb.GradeCardRequest, _ ...grpc.CallOption) (*pb.GradeCardResponse, error) {
	c.grades++
	result, err := c.server.GradeCard(ctx, req)
	if err == nil && c.loseGrade {
		c.loseGrade = false
		return nil, errors.New("lost grade reply")
	}
	return result, err
}

type englishFlowCore struct{ clients.CoreService }

func (*englishFlowCore) GetTodayDate(context.Context) (string, error) { return "14-Nov-2026", nil }

type englishFlowLLM struct {
	clients.LLMService
	calls                    int
	input, instruction, kind string
	previous                 *clients.EnglishDraft
	fail                     bool
}

func (l *englishFlowLLM) PrepareEnglish(_ context.Context, kind, input, instruction string, previous *clients.EnglishDraft) (*clients.EnglishDraft, error) {
	l.calls++
	l.input = input
	l.instruction = instruction
	l.previous = previous
	l.kind = kind
	if l.fail {
		return nil, errors.New("synthetic model unavailable")
	}
	if kind == "word" && input == "yield" && instruction == "" {
		return &clients.EnglishDraft{Question: "Урожай или уступить дорогу?"}, nil
	}
	if kind == "word" {
		return &clients.EnglishDraft{Source: "уступать дорогу", English: "yield", Example: "Yield to pedestrians.", Explanation: "На дороге", Context: "road sign"}, nil
	}
	return &clients.EnglishDraft{Source: "Я потратил полчаса, пытаясь это исправить", English: "I spent half an hour trying to fix it.", Explanation: "spend time doing something; spent — прошедшая форма", Example: "I spent an hour reading.", Context: "<context> & details"}, nil
}
func englishFlowFixture(t *testing.T) (*App, *englishFlowClient, *englishFlowLLM, *durableVoiceAPI, *telego.Bot, *noteFlowState, *telego.CallbackQuery) {
	t.Helper()
	repo, whisper, voiceCore, api, bot := durableVoiceFixture(t)
	app := durableVoiceApp(repo, whisper, voiceCore)
	client := &englishFlowClient{server: &core.EnglishServer{Vault: t.TempDir()}}
	llm := &englishFlowLLM{}
	app.English = client
	app.Tutor = llm
	app.Core = &englishFlowCore{}
	state := app.State.(*noteFlowState)
	state.value.ActiveDate = "01-Jan-2020"
	q := &telego.CallbackQuery{From: telego.User{ID: 42}, Message: &telego.Message{MessageID: 71, Chat: telego.Chat{ID: 42}}}
	return app, client, llm, api, bot, state, q
}
func englishAction(t *testing.T, a *App, b *telego.Bot, q *telego.CallbackQuery, parts ...string) {
	t.Helper()
	require.NoError(t, a.handleEnglishAction(t.Context(), b, q, 42, append([]string{"english"}, parts...)))
}
func englishText(t *testing.T, a *App, b *telego.Bot, text string) {
	t.Helper()
	a.HandleTextMessage(t.Context(), b, &telego.Update{Message: &telego.Message{MessageID: 99, From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Text: text}})
}
func TestEnglishPhrasePreviewSaveAndSequentialInput(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	englishAction(t, app, bot, q, "start", "phrase")
	englishText(t, app, bot, "Я потратил полчаса, пытаясь это исправить <&>")
	require.Equal(t, 1, llm.calls)
	require.Equal(t, 0, client.saves)
	require.Equal(t, "14-Nov-2026", state.value.English.Date)
	require.Contains(t, api.texts[len(api.texts)-1], "&lt;&amp;&gt;")
	require.Contains(t, api.texts[len(api.texts)-1], "&lt;context&gt; &amp; details")
	initial := state.value.English.Nonce
	englishAction(t, app, bot, q, "example", initial)
	require.Contains(t, llm.instruction, "Ещё пример")
	require.NotNil(t, llm.previous)
	englishAction(t, app, bot, q, "save", initial)
	require.Zero(t, client.saves)
	nonce := state.value.English.Nonce
	raw, err := json.Marshal(state.value)
	require.NoError(t, err)
	state.value = tgstates.UserContext{}
	require.NoError(t, json.Unmarshal(raw, &state.value))
	client.loseSave = true
	require.NoError(t, app.handleEnglishAction(t.Context(), bot, q, 42, []string{"english", "save", nonce}))
	require.Contains(t, api.markups[len(api.markups)-1], "english:save:"+nonce)
	englishAction(t, app, bot, q, "save", nonce)
	require.Equal(t, tgstates.StateEnglishInput, state.value.State)
	englishAction(t, app, bot, q, "save", nonce)
	require.Equal(t, 2, client.saves)
	data, err := os.ReadFile(filepath.Join(client.server.Vault, "Tags/English/phrases.md"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "I spent half an hour trying to fix it."))
	require.Contains(t, string(data), "[[14-Nov-2026]]")
	englishText(t, app, bot, "Следующая мысль")
	require.Equal(t, "Следующая мысль", llm.input)
	require.Equal(t, tgstates.StateEnglishPreview, state.value.State)
	require.NoDirExists(t, filepath.Join(client.server.Vault, "Daily"))
}
func TestEnglishWordAmbiguityClarificationAndCorrection(t *testing.T) {
	app, client, llm, _, bot, state, q := englishFlowFixture(t)
	englishAction(t, app, bot, q, "start", "word")
	englishText(t, app, bot, "yield")
	require.NotEmpty(t, state.value.English.Question)
	englishAction(t, app, bot, q, "save", state.value.English.Nonce)
	require.Zero(t, client.saves)
	englishAction(t, app, bot, q, "clarify", state.value.English.Nonce)
	englishText(t, app, bot, "На дорожном знаке")
	require.Equal(t, "yield", llm.input)
	require.Equal(t, "На дорожном знаке", llm.instruction)
	require.Contains(t, state.value.English.Draft.Context, "yield / На дорожном знаке")
	englishAction(t, app, bot, q, "clarify", state.value.English.Nonce)
	englishText(t, app, bot, "Исправление: yield to pedestrians")
	require.Equal(t, "yield to pedestrians", llm.input)
	require.Empty(t, llm.instruction)
	require.Nil(t, llm.previous)
	englishAction(t, app, bot, q, "save", state.value.English.Nonce)
	cards, err := client.ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, cards.Cards, 1)
	require.Equal(t, "уступать дорогу", cards.Cards[0].Source)
}
func TestEnglishModelFailureKeepsInputAndOffersRetry(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	llm.fail = true
	englishAction(t, app, bot, q, "start", "phrase")
	englishText(t, app, bot, "Моя мысль")
	require.Equal(t, "Моя мысль", state.value.English.Input)
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishFailure)
	require.Zero(t, client.saves)
	llm.fail = false
	englishAction(t, app, bot, q, "retry", state.value.English.Nonce)
	require.NotNil(t, state.value.English.Draft)
}
func TestEnglishReviewSelfAssessmentNoModelAndNoDiary(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	path := filepath.Join(client.server.Vault, "Tags/English")
	require.NoError(t, os.MkdirAll(path, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "words.md"), []byte("[[10-Oct-2026]]\n- `gate` - выход на посадку\n- `aisle` - проход\n- `layover` - пересадка\n- `yield` - уступить дорогу\n- `boarding` - посадка\n"), 0644))
	englishAction(t, app, bot, q, "review")
	require.Contains(t, api.texts[len(api.texts)-1], "5 (слова: 5, фразы: 0)")
	englishAction(t, app, bot, q, "queue", "all")
	e := state.value.English
	require.NotContains(t, api.texts[len(api.texts)-1], "gate")
	require.Contains(t, api.texts[len(api.texts)-1], "выход на посадку")
	englishAction(t, app, bot, q, "grade", e.Nonce, "easy")
	require.Zero(t, client.grades)
	englishAction(t, app, bot, q, "hint", e.Nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "g…")
	require.NotContains(t, api.texts[len(api.texts)-1], "gate")
	englishAction(t, app, bot, q, "show", e.Nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "gate")
	client.loseGrade = true
	require.NoError(t, app.handleEnglishAction(t.Context(), bot, q, 42, []string{"english", "grade", e.Nonce, "miss"}))
	require.Contains(t, api.markups[len(api.markups)-1], "english:grade:"+e.Nonce+":miss")
	englishAction(t, app, bot, q, "grade", e.Nonce, "miss")
	require.Len(t, state.value.English.Queue, 5)
	require.Equal(t, e.Current.Id, state.value.English.Queue[3])
	englishAction(t, app, bot, q, "grade", e.Nonce, "easy")
	require.Equal(t, 2, client.grades)
	englishAction(t, app, bot, q, "next", e.Nonce)
	newNonce := state.value.English.Nonce
	englishAction(t, app, bot, q, "grade", e.Nonce, "miss")
	require.Equal(t, newNonce, state.value.English.Nonce)
	require.Equal(t, 2, client.grades)
	englishAction(t, app, bot, q, "skip", newNonce)
	require.Equal(t, 2, client.grades)
	englishText(t, app, bot, "I remember this")
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishReviewNoInput)
	englishAction(t, app, bot, q, "menu")
	require.Zero(t, llm.calls)
	cards, err := client.ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	for _, c := range cards.Cards {
		if c.Id == e.Current.Id {
			require.EqualValues(t, 1, c.Lapses)
			require.EqualValues(t, 600, c.IntervalSeconds)
		}
	}
	require.NoDirExists(t, filepath.Join(client.server.Vault, "Daily"))
	require.Empty(t, englishRetryQueue(nil, "forgotten")) // A single card is due soon, no forced immediate loop.
}
func TestEnglishVoiceSurvivesModeChangeAndRestart(t *testing.T) {
	for _, kind := range []string{"word", "phrase"} {
		t.Run(kind, func(t *testing.T) {
			app, client, llm, api, bot, state, q := englishFlowFixture(t)
			englishAction(t, app, bot, q, "start", kind)
			if kind == "phrase" {
				app.Whisper.(*durableVoiceWhisper).result.Text = "Я хочу <voice> & помощи"
			}
			sent := time.Date(2026, 11, 14, 0, 30, 0, 0, time.UTC)
			update := &telego.Update{Message: &telego.Message{MessageID: 7, Date: sent.Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "original"}}}
			require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
			repo := app.VoiceJobs.(*durableVoiceRepo)
			j, err := repo.Find(t.Context(), 42, 7)
			require.NoError(t, err)
			require.Equal(t, "english:"+kind, j.Mode)
			require.Equal(t, "13-Nov-2026", j.ActiveDate)
			state.value = tgstates.UserContext{State: tgstates.StateIdle, ActiveDate: "01-Jan-2020"}
			require.NoError(t, app.AcceptVoice(t.Context(), bot, update))
			require.Len(t, repo.jobs, 1)
			done := false
			for attempt := 0; attempt < 15 && !done; attempt++ {
				step, err := repo.Get(t.Context(), j.ID, 42)
				require.NoError(t, err)
				restarted := App{Cfg: app.Cfg, Core: app.Core, English: app.English, LLM: app.LLM, Tutor: app.Tutor, Whisper: app.Whisper, VoiceJobs: app.VoiceJobs, Logger: app.Logger}
				restarted.State = &noteFlowState{} // No UI or in-memory voice cache is consulted.
				var delay time.Duration
				done, delay, err = restarted.voiceStep(t.Context(), bot, step)
				require.NoError(t, err)
				require.NoError(t, repo.Release(t.Context(), step, done, delay))
			}
			require.True(t, done)
			require.Contains(t, api.texts[len(api.texts)-1], r.EnglishVoiceReady)
			require.Equal(t, 1, llm.calls)
			job, err := repo.Get(t.Context(), j.ID, 42)
			require.NoError(t, err)
			require.False(t, job.NoteSaved)
			require.NoError(t, app.openDurableVoice(t.Context(), bot, q, 42, j.ID))
			require.Equal(t, kind, state.value.English.Kind)
			if kind == "phrase" {
				require.Contains(t, api.texts[len(api.texts)-1], "Я хочу &lt;voice&gt; &amp; помощи")
			} else {
				require.Contains(t, api.texts[len(api.texts)-1], "synthetic &lt;voice&gt; &amp; result")
			}
			englishAction(t, app, bot, q, "save", state.value.English.Nonce)
			require.NoError(t, app.openDurableVoice(t.Context(), bot, q, 42, j.ID))
			englishAction(t, app, bot, q, "save", state.value.English.Nonce)
			cards, err := client.ListCards(t.Context(), &pb.ListCardsRequest{})
			require.NoError(t, err)
			require.Len(t, cards.Cards, 1)
			require.Equal(t, "13-Nov-2026", cards.Cards[0].Date)
			require.NoDirExists(t, filepath.Join(client.server.Vault, "Daily"))
		})
	}
}
func TestEnglishVoiceClarificationUsesSnapshotAndOutageKeepsTranscript(t *testing.T) {
	app, _, llm, _, _, _, _ := englishFlowFixture(t)
	e := tgstates.EnglishContext{Kind: "word", Input: "yield", Date: "12-Nov-2026"}
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	job := &voicejobs.Job{ID: "synthetic", Mode: "english:word", LearningState: string(tgstates.StateEnglishClarify), LearningContext: raw, Text: "На дороге"}
	prepared, err := app.prepareVoiceIntent(t.Context(), job)
	require.NoError(t, err)
	require.Equal(t, "yield", llm.input)
	require.Equal(t, "На дороге", llm.instruction)
	var p voicePrepared
	require.NoError(t, json.Unmarshal(prepared, &p))
	require.Equal(t, "12-Nov-2026", p.English.Date)
	job.Text = "Исправление: yield to pedestrians"
	_, err = app.prepareVoiceIntent(t.Context(), job)
	require.NoError(t, err)
	require.Equal(t, "yield to pedestrians", llm.input)
	require.Empty(t, llm.instruction)
	llm.fail = true
	prepared, err = app.prepareVoiceIntent(t.Context(), job)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(prepared, &p))
	require.Equal(t, r.EnglishFailure, p.English.Question)
	require.Equal(t, job.Text, p.English.Transcript)
}
func TestEnglishReviewVoiceCannotFallThroughToDiary(t *testing.T) {
	app, _, _, _, bot, state, _ := englishFlowFixture(t)
	for _, s := range []tgstates.UserState{tgstates.StateEnglishMenu, tgstates.StateEnglishReview} {
		state.value.State = s
		require.NoError(t, app.AcceptVoice(t.Context(), bot, &telego.Update{Message: &telego.Message{MessageID: 7, From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "synthetic"}}}))
	}
	require.Empty(t, app.VoiceJobs.(*durableVoiceRepo).jobs)
}

func (*englishFlowCore) EnsureNote(context.Context, string) (bool, error) { return true, nil }
func (*englishFlowCore) GetNote(context.Context, string) (string, error) {
	return "synthetic daily note", nil
}
func (*englishFlowCore) GetRating(context.Context, string) (int, bool, error) { return 0, false, nil }
func TestEnglishSwitchingToNoteChangesNewVoiceDestination(t *testing.T) {
	app, _, _, _, bot, state, q := englishFlowFixture(t)
	englishAction(t, app, bot, q, "start", "word")
	require.NoError(t, app.handleMenuAction(t.Context(), bot, q, 42, []string{"menu", "note"}))
	require.Equal(t, tgstates.StateIdle, state.value.State)
	require.NoError(t, app.AcceptVoice(t.Context(), bot, &telego.Update{Message: &telego.Message{MessageID: 44, Date: time.Now().Unix(), From: &telego.User{ID: 42}, Chat: telego.Chat{ID: 42}, Voice: &telego.Voice{FileID: "new daily voice"}}}))
	job, err := app.VoiceJobs.Find(t.Context(), 42, 44)
	require.NoError(t, err)
	require.Equal(t, "note", job.Mode)
}

func TestEnglishPhraseUsesUserThoughtAndSeparatesAttempt(t *testing.T) {
	app, _, llm, _, bot, state, q := englishFlowFixture(t)
	englishAction(t, app, bot, q, "start", "phrase")
	thought := "Я потратил полчаса, пытаясь это исправить"
	input := thought + "\nМой вариант: I spend half hour to fix it."
	englishText(t, app, bot, input)
	require.Equal(t, input, llm.input)
	require.Equal(t, thought, state.value.English.Draft.Source)
	require.NotContains(t, state.value.English.Draft.Source, "I spend")
	englishAction(t, app, bot, q, "clarify", state.value.English.Nonce)
	englishText(t, app, bot, "исправление. Я потратил час, пытаясь это исправить")
	require.Equal(t, "Я потратил час, пытаясь это исправить", state.value.English.Draft.Source)
	englishAction(t, app, bot, q, "new", state.value.English.Nonce)
	englishText(t, app, bot, "Я хочу кофе. I want coffee.")
	require.Equal(t, r.EnglishPhraseSourcePrompt, state.value.English.Question)
	require.Nil(t, state.value.English.Draft)
	englishText(t, app, bot, "Я хочу кофе.")
	require.Empty(t, state.value.English.Question)
	require.Equal(t, "Я хочу кофе.", state.value.English.Draft.Source)
}
