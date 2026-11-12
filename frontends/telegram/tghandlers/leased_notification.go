package tghandlers

import (
	"context"
	"time"

	"github.com/mymmrac/telego"

	"notes-bot/frontends/telegram/tgfmt"
)

// Both geofence and task actions share the send deadline and acknowledgement policy.
func (a *App) sendLeasedNotification(ctx context.Context, b *telego.Bot, text tgfmt.HTML, expiry time.Time, complete func(context.Context, bool) error) error {
	deadline := time.Now().Add(20 * time.Second)
	if expiry.Before(deadline) {
		deadline = expiry
	}
	sendErr := context.DeadlineExceeded
	if expiry.After(time.Now()) {
		sendCtx, cancel := context.WithDeadline(ctx, deadline)
		sendErr = sendText(sendCtx, b, a.Cfg.RootID, text, nil, false)
		cancel()
	}
	ackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := complete(ackCtx, sendErr == nil); err != nil {
		return err
	}
	return sendErr
}
