package notifications

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type notifMetrics struct {
	remindersFired  metric.Int64Counter
	publishErrors   metric.Int64Counter
	tickDuration    metric.Float64Histogram
	rpcRequests     metric.Int64Counter
	locationUpdates metric.Int64Counter
	locationTrackingGauges
}

type locationTrackingGauges struct {
	activeTrackers metric.Int64ObservableGauge
	latestLat      metric.Float64ObservableGauge
	latestLon      metric.Float64ObservableGauge
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
	locationUpdates, _ := meter.Int64Counter("notifications.location.updates.total",
		metric.WithDescription("Total location updates received"),
	)

	m := &notifMetrics{
		remindersFired:  remindersFired,
		publishErrors:   publishErrors,
		tickDuration:    tickDuration,
		rpcRequests:     rpcRequests,
		locationUpdates: locationUpdates,
	}
	m.locationTrackingGauges = locationTrackingGauges{
		activeTrackers: nil,
		latestLat:      nil,
		latestLon:      nil,
	}

	return m
}

func (s *notifMetrics) RecordLocationUpdate(ctx context.Context, source string) {
	s.locationUpdates.Add(ctx, 1,
		metric.WithAttributes(attribute.String("source", source)),
	)
}

type locationGauge struct {
	lat, lon float64
}

var globalLocationGauge = &locationGauge{}

func (s *notifMetrics) ObserveLatestLocation(lat, lon float64) {
	globalLocationGauge.lat = lat
	globalLocationGauge.lon = lon
}
