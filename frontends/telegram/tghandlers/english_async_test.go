package tghandlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"notes-bot/frontends/telegram/clients"
	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
)

type englishGeneratorFunc func(context.Context, string, string, string, *clients.EnglishDraft) (*clients.EnglishDraft, error)

func (f englishGeneratorFunc) PrepareEnglish(ctx context.Context, kind, input, instruction string, previous *clients.EnglishDraft) (*clients.EnglishDraft, error) {
	return f(ctx, kind, input, instruction, previous)
}

// Exercise the same per-user serialization as the Telegram update dispatcher.
func englishUpdate(t *testing.T, app *App, update func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		unlock := app.LockUser(42)
		defer unlock()
		done <- update()
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("update blocked while model was preparing a draft")
	}
}

func TestEnglishGenerationAllowsNavigationAndDiscardsLateResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			app, _, _, api, bot, state, q := englishFlowFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			release := make(chan struct{})
			started := make(chan struct{})
			app.Tutor = englishGeneratorFunc(func(ctx context.Context, _, _, _ string, _ *clients.EnglishDraft) (*clients.EnglishDraft, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				if fail {
					return nil, errors.New("synthetic failure")
				}
				return &clients.EnglishDraft{English: "late answer"}, nil
			})
			t.Cleanup(func() { cancel(); app.WaitEnglishGenerations() })
			englishAction(t, app, bot, q, "start", "word")
			englishUpdate(t, app, func() error {
				return app.englishInput(ctx, bot, 42, 42, "road sign", &state.value)
			})
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("model request did not start")
			}
			require.Equal(t, r.EnglishPreparing, api.texts[len(api.texts)-1])
			englishUpdate(t, app, func() error {
				return app.handleMenuAction(ctx, bot, q, 42, []string{"menu", "rating"})
			})
			require.Equal(t, tgstates.StateWaitingRating, state.value.State)
			messages := len(api.texts)
			close(release)
			app.WaitEnglishGenerations()
			require.Equal(t, tgstates.StateWaitingRating, state.value.State)
			require.Len(t, api.texts, messages, "late success/failure must not replace the new screen")
		})
	}
}

func TestEnglishGenerationNewInputSupersedesPendingRequest(t *testing.T) {
	app, _, _, api, bot, state, q := englishFlowFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	releaseOld := make(chan struct{})
	app.Tutor = englishGeneratorFunc(func(ctx context.Context, _, input, _ string, _ *clients.EnglishDraft) (*clients.EnglishDraft, error) {
		if input == "old" {
			close(oldStarted)
			<-ctx.Done()
			close(oldCanceled)
			// Simulate a backend returning a late response despite cancellation.
			select {
			case <-releaseOld:
			case <-time.After(3 * time.Second):
			}
		}
		return &clients.EnglishDraft{English: input}, nil
	})
	t.Cleanup(func() { cancel(); app.WaitEnglishGenerations() })
	englishAction(t, app, bot, q, "start", "word")
	englishUpdate(t, app, func() error { return app.englishInput(ctx, bot, 42, 42, "old", &state.value) })
	select {
	case <-oldStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("old request did not start")
	}
	englishUpdate(t, app, func() error {
		if err := app.handleEnglishAction(ctx, bot, q, 42, []string{"english", "new", state.value.English.Nonce}); err != nil {
			return err
		}
		return app.englishInput(ctx, bot, 42, 42, "new", &state.value)
	})
	select {
	case <-oldCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("superseded model request was not canceled")
	}
	close(releaseOld)
	app.WaitEnglishGenerations()
	require.Equal(t, "new", state.value.English.Input)
	require.Equal(t, "new", state.value.English.Draft.English)
	require.NotContains(t, api.texts[len(api.texts)-1], "old")
}

func TestEnglishGenerationStopsOnShutdown(t *testing.T) {
	app, _, _, api, bot, state, q := englishFlowFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	app.Tutor = englishGeneratorFunc(func(ctx context.Context, _, _, _ string, _ *clients.EnglishDraft) (*clients.EnglishDraft, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	t.Cleanup(func() { cancel(); app.WaitEnglishGenerations() })
	englishAction(t, app, bot, q, "start", "word")
	englishUpdate(t, app, func() error { return app.englishInput(ctx, bot, 42, 42, "pending", &state.value) })
	cancel()
	done := make(chan struct{})
	go func() { app.WaitEnglishGenerations(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel the model request")
	}
	require.Equal(t, r.EnglishPreparing, api.texts[len(api.texts)-1])
}
