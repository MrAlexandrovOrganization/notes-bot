package tghandlers

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/timeutil"
)

// App holds shared dependencies for all handlers.
type App struct {
	Cfg           *config.Config
	Core          clients.CoreService
	Notifications clients.NotificationsService
	Whisper       clients.WhisperService
	Search        clients.SearchService
	LLM           clients.LLMService
	Location      clients.LocationService
	State         tgstates.StateStore
	Logger        *zap.Logger

	// voiceCancels stores cancel functions for in-progress transcription jobs.
	// Key: jobID (string), Value: context.CancelFunc.
	voiceCancels sync.Map

	// voiceTexts stores completed transcription texts for pagination.
	// Key: statusMsgID (int), Value: string. Bounded via voiceTextsOrder.
	voiceTexts sync.Map

	// voiceTextsOrder tracks insertion order into voiceTexts for eviction.
	voiceTextsOrderMu sync.Mutex
	voiceTextsOrder   []int

	// voiceBuffers holds per-user reorder buffers that ensure transcription
	// results are delivered in Telegram MessageID order (= user send order).
	// Key: userID (int64), Value: *voiceReorderBuffer.
	voiceBuffers sync.Map

	// userMu provides per-user mutexes that serialize FULL update handling
	// (not just state writes). Without them two concurrent callbacks from
	// the same user could both act on a stale state snapshot taken before
	// either wrote its changes.
	userMu sync.Map // map[int64]*sync.Mutex
}

// LockUser acquires the per-user handler mutex and returns the unlock func.
// Exported: cmd/telegram serializes whole-update handling with it.
func (a *App) LockUser(userID int64) func() {
	v, _ := a.userMu.LoadOrStore(userID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// authorized returns true if the userID is allowed to use the bot.
// RootID <= 0 must never grant access (config validation rejects it, but this
// is the last line of defense for a personal bot).
func (a *App) authorized(userID int64) bool {
	return a.Cfg.RootID > 0 && userID == a.Cfg.RootID
}

// updateState wraps State.UpdateContext with error logging so that Redis
// failures are never silently swallowed.
func (a *App) updateState(ctx context.Context, userID int64, updates func(*tgstates.UserContext)) {
	if err := a.State.UpdateContext(ctx, userID, updates); err != nil {
		applog.With(ctx, a.Logger).Error("failed to update user state",
			zap.Int64("user_id", userID), zap.Error(err))
	}
}

// setActiveDate wraps State.SetActiveDate with error logging.
func (a *App) setActiveDate(ctx context.Context, userID int64, date string) {
	if err := a.State.SetActiveDate(ctx, userID, date); err != nil {
		applog.With(ctx, a.Logger).Error("failed to set active date",
			zap.Int64("user_id", userID), zap.String("date", date), zap.Error(err))
	}
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
