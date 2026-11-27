package main

import (
	"context"
	"errors"
	"os"
	"time"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/webapp"
	"notes-bot/internal/env"
	"notes-bot/internal/mobilevoice"
	"notes-bot/internal/voicejobs"
)

func startVoice(ctx context.Context, app *webapp.App, core *clients.CoreClient, mobileOnly bool) (func(), <-chan struct{}, error) {
	if os.Getenv("DB_PASSWORD") == "" {
		return nil, nil, errors.New("DB_PASSWORD is required for voice jobs")
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := voicejobs.NewPool(dbCtx,
		env.Str("DB_HOST", "localhost"), env.Str("DB_PORT", "5432"),
		env.Str("DB_NAME", "notifications"), env.Str("DB_USER", "notif"), os.Getenv("DB_PASSWORD"))
	if err != nil {
		return nil, nil, err
	}
	store := &mobilevoice.Store{Pool: pool}
	if !mobileOnly {
		store = mobilevoice.NewWebStore(pool)
	}
	if err := store.CheckSchema(dbCtx); err != nil {
		pool.Close()
		return nil, nil, err
	}
	whisper, err := clients.NewWhisperClient(env.Str("WHISPER_GRPC_HOST", "whisper-ingress"), env.Str("WHISPER_GRPC_PORT", "50053"))
	if err != nil {
		pool.Close()
		return nil, nil, errors.New("failed to configure voice Transcriber client")
	}
	app.Voice = store
	worker := mobilevoice.NewWorker(store, whisper, core, app.Logger)
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	return func() {
		whisper.Close()
		pool.Close()
	}, done, nil
}
