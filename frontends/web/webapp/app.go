// Package webapp implements the web frontend's HTTP handlers on top of the
// same gRPC/HTTP clients used by the Telegram frontend (notes-bot/frontends/telegram/clients).
package webapp

import (
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/config"
)

// singleUserID is the userID used for all Notifications RPCs. The web
// frontend, like the Telegram bot, serves exactly one person — there is no
// multi-tenant user model. 0 keeps reminders separate from any real Telegram
// user ID (which is always positive).
const singleUserID = int64(0)

// App holds shared dependencies for all HTTP handlers.
type App struct {
	Cfg           *config.Config
	Core          clients.CoreService
	Notifications clients.NotificationsService
	Search        clients.SearchService
	LLM           clients.LLMService
	Logger        *zap.Logger
}
