package automations

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (s *Store) RegisterMetrics() error {
	meter := otel.Meter("notifications/automations")
	states, err := meter.Int64ObservableGauge("notifications.automation.executions", metric.WithDescription("Automation action executions by persisted status"))
	if err != nil {
		return err
	}
	age, err := meter.Float64ObservableGauge("notifications.automation.oldest.pending.age", metric.WithUnit("s"), metric.WithDescription("Age of the oldest due, unexpired action"))
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		rows, err := s.Pool.Query(ctx, query("metrics"))
		if err != nil {
			return err
		}
		counts := map[string]int64{"pending": 0, "sending": 0, "sent": 0, "expired": 0, "cancelled": 0}
		for rows.Next() {
			var state string
			var n int64
			if err = rows.Scan(&state, &n); err != nil {
				rows.Close()
				return err
			}
			counts[state] = n
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		var seconds float64
		if err = s.Pool.QueryRow(ctx, query("oldest")).Scan(&seconds); err != nil {
			return err
		}
		for state, n := range counts {
			o.ObserveInt64(states, n, metric.WithAttributes(attribute.String("status", state)))
		}
		o.ObserveFloat64(age, seconds)
		return nil
	}, states, age)
	return err
}
