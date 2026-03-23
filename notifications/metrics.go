package notifications

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type notifMetrics struct {
	remindersFired metric.Int64Counter
	publishErrors  metric.Int64Counter
	tickDuration   metric.Float64Histogram
	rpcRequests    metric.Int64Counter
}

func newNotifMetrics() *notifMetrics {
	meter := otel.GetMeterProvider().Meter("notifications")

	remindersFired, _ := meter.Int64Counter("notifications.reminders.fired",
		metric.WithDescription("Total reminders fired by schedule type"),
	)
	publishErrors, _ := meter.Int64Counter("notifications.kafka.publish.errors",
		metric.WithDescription("Total Kafka publish errors"),
	)
	tickDuration, _ := meter.Float64Histogram("notifications.scheduler.tick.duration",
		metric.WithDescription("Scheduler tick duration"),
		metric.WithUnit("s"),
	)
	rpcRequests, _ := meter.Int64Counter("notifications.rpc.requests",
		metric.WithDescription("Total gRPC requests by method and status"),
	)

	return &notifMetrics{
		remindersFired: remindersFired,
		publishErrors:  publishErrors,
		tickDuration:   tickDuration,
		rpcRequests:    rpcRequests,
	}
}
