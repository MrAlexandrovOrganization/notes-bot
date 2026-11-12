package automations

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
)

func (s *Server) Run(ctx context.Context, onError func()) {
	failures, _ := otel.Meter("notifications/automations").Int64Counter("notifications.automation.poll.failures")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Poll(ctx); err != nil && ctx.Err() == nil {
			failures.Add(ctx, 1)
			onError()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
