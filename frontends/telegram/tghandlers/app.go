package tghandlers

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/timeutil"
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

	// voiceTexts stores completed transcription texts for pagination.
	// Key: statusMsgID (int), Value: string.
	voiceTexts sync.Map

	// voiceBuffers holds per-user reorder buffers that ensure transcription
	// results are delivered in Telegram MessageID order (= user send order).
	// Key: userID (int64), Value: *voiceReorderBuffer.
	voiceBuffers sync.Map
}

// authorized returns true if the userID is allowed to use the bot.
func (a *App) authorized(userID int64) bool {
	return a.Cfg.RootID == 0 || userID == a.Cfg.RootID
}

// llmDateContext возвращает четыре строки, которые ждёт LLMService:
//   - currentDateTime: "YYYY-MM-DD HH:MM" — сейчас в локальной TZ
//   - today/tomorrow/dayAfter: "YYYY-MM-DD" — логические даты с учётом DAY_START_HOUR
func (a *App) llmDateContext() (currentDateTime, today, tomorrow, dayAfter string) {
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	logical := timeutil.LogicalToday(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour)
	const iso = "2006-01-02"
	return now.Format("2006-01-02 15:04"),
		logical.Format(iso),
		logical.AddDate(0, 0, 1).Format(iso),
		logical.AddDate(0, 0, 2).Format(iso)
}

// cancelVoiceJob cancels a running transcription job and notifies the backend.
func (a *App) cancelVoiceJob(ctx context.Context, jobID string) {
	val, ok := a.voiceCancels.LoadAndDelete(jobID)
	if !ok {
		return
	}
	cancel, ok := val.(context.CancelFunc)
	if !ok {
		a.Logger.Error("invalid cancel function type in voiceCancels", zap.String("job_id", jobID))
		return
	}
	cancel()
	if _, err := a.Whisper.Cancel(ctx, jobID); err != nil {
		a.Logger.Warn("cancel whisper job", zap.String("job_id", jobID), zap.Error(err))
	}
}
