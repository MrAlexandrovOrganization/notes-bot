// Package webapp implements the web frontend's HTTP handlers on top of the
// same gRPC/HTTP clients used by the Telegram frontend (notes-bot/frontends/telegram/clients).
package webapp

import (
	"sync"
	"time"

	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/config"
)

// App holds shared dependencies for all HTTP handlers.
type App struct {
	Cfg           *config.Config
	Core          clients.CoreService
	Notifications clients.NotificationsService
	Search        clients.SearchService
	LLM           clients.LLMService
	Logger        *zap.Logger

	// revokedSessions tracks logged-out session IDs until their natural
	// expiry, so a stolen cookie cannot outlive a logout.
	sessionsMu      sync.Mutex
	revokedSessions map[string]time.Time

	// loginAttempts bounds password guessing: remote IP -> attempt timestamps.
	loginMu       sync.Mutex
	loginFailures map[string][]time.Time
}
