package georeminders

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

func (s *Store) RegisterMetrics() error {
	meter := otel.Meter("notifications/geofences")
	deliveries, err := meter.Int64ObservableGauge("notifications.geo.deliveries", metric.WithDescription("Location notification deliveries by persisted status"))
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		rows, err := s.Pool.Query(ctx, query("delivery_counts"))
		if err != nil {
			return err
		}
		defer rows.Close()
		counts := map[string]int64{"pending": 0, "sending": 0, "sent": 0, "expired": 0, "cancelled": 0}
		for rows.Next() {
			var status string
			var n int64
			if err := rows.Scan(&status, &n); err != nil {
				return err
			}
			counts[status] = n
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for status, n := range counts {
			o.ObserveInt64(deliveries, n, metric.WithAttributes(attribute.String("status", status)))
		}
		return nil
	}, deliveries)
	return err
}
