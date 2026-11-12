package core

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

func RegisterTaskEventMetrics(ctx context.Context) error {
	dir, err := taskOutboxDir(ctx)
	if err != nil {
		return err
	}
	_, err = otel.Meter("core/task-events").Int64ObservableGauge("core.automation.events.pending", metric.WithDescription("Durable task events awaiting consumer acknowledgement"), metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var n int64
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				n++
			}
		}
		o.Observe(n)
		return nil
	}))
	return err
}
