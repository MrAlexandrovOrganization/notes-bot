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
	Core          *clients.CoreClient
	Notifications *clients.NotificationsClient
	Whisper       *clients.WhisperClient
	State         *tgstates.StateManager
	Logger        *zap.Logger
}
