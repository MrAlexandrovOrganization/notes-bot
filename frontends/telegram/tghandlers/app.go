package tghandlers

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgstates"
)

// App holds shared dependencies for all handlers.
type App struct {
	Cfg           *config.Config
	Core          clients.CoreService
	Notifications clients.NotificationsService
	Whisper       clients.WhisperService
	LLM           clients.LLMService
	State         tgstates.StateStore
	Logger        *zap.Logger

	// voiceCancels stores cancel functions for in-progress transcription jobs.
	// Key: jobID (string), Value: context.CancelFunc.
	voiceCancels sync.Map
}

// authorized returns true if the userID is allowed to use the bot.
func (a *App) authorized(userID int64) bool {
	return a.Cfg.RootID == 0 || userID == a.Cfg.RootID
}

// cancelVoiceJob cancels a running transcription job and notifies the backend.
func (a *App) cancelVoiceJob(ctx context.Context, jobID string) {
	val, ok := a.voiceCancels.LoadAndDelete(jobID)
	if !ok {
		return
	}
	val.(context.CancelFunc)()
	if _, err := a.Whisper.Cancel(ctx, jobID); err != nil {
		a.Logger.Warn("cancel whisper job", zap.String("job_id", jobID), zap.Error(err))
	}
}
