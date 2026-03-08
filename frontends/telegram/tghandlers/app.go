package tghandlers

import (
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/clients"
	"notes_bot/frontends/telegram/config"
	"notes_bot/frontends/telegram/tgstates"
)

// App holds shared dependencies for all handlers.
type App struct {
	Cfg           *config.Config
	Core          clients.CoreService
	Notifications clients.NotificationsService
	Whisper       clients.WhisperService
	State         *tgstates.StateManager
	Logger        *zap.Logger
}

// authorized returns true if the userID is allowed to use the bot.
func (a *App) authorized(userID int64) bool {
	return a.Cfg.RootID == 0 || userID == a.Cfg.RootID
}
