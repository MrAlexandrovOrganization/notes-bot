package bot

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Package-level metrics for the Telegram service.
// Call InitTelegramMetrics() once after telemetry.InitMetrics() to register.
var (
	UpdatesTotal           metric.Int64Counter
	KafkaMessagesConsumed  metric.Int64Counter
	ReminderDeliveryErrors metric.Int64Counter
	HandlerDuration        metric.Float64Histogram
)

// InitTelegramMetrics registers all Telegram service metric instruments
// using the current global MeterProvider. Must be called after telemetry.InitMetrics().
func InitTelegramMetrics() {
	meter := otel.GetMeterProvider().Meter("telegram")

	UpdatesTotal, _ = meter.Int64Counter("telegram.updates",
		metric.WithDescription("Total Telegram updates processed by type"),
	)
	KafkaMessagesConsumed, _ = meter.Int64Counter("telegram.kafka.messages.consumed",
		metric.WithDescription("Total Kafka reminder messages consumed"),
	)
	ReminderDeliveryErrors, _ = meter.Int64Counter("telegram.reminder.delivery.errors",
		metric.WithDescription("Total reminder notification delivery errors"),
	)
	HandlerDuration, _ = meter.Float64Histogram("telegram.handler.duration",
		metric.WithDescription("Telegram update handler duration"),
		metric.WithUnit("s"),
	)
}
