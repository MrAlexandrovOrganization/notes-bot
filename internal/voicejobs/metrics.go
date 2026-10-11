package voicejobs

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// RegisterMetrics observes durable queue state with one bounded DB query per
// collection. Call after installing the application's MeterProvider and
// unregister the returned callback before closing the pool.
func (s *Store) RegisterMetrics() (metric.Registration, error) {
	m := otel.GetMeterProvider().Meter("telegram")
	pending, err := m.Int64ObservableGauge("telegram.voice.jobs.pending", metric.WithDescription("Accepted voice jobs awaiting terminal delivery"))
	if err != nil {
		return nil, err
	}
	ready, err := m.Int64ObservableGauge("telegram.voice.jobs.ready_undelivered", metric.WithDescription("Locally persisted voice results awaiting terminal delivery"))
	if err != nil {
		return nil, err
	}
	cancelled, err := m.Int64ObservableGauge("telegram.voice.jobs.cancelled_pending", metric.WithDescription("Cancellation requests awaiting terminal delivery"))
	if err != nil {
		return nil, err
	}
	age, err := m.Float64ObservableGauge("telegram.voice.jobs.oldest_pending.age", metric.WithUnit("s"), metric.WithDescription("Age since acceptance of the oldest pending voice job"))
	if err != nil {
		return nil, err
	}
	available, err := m.Int64ObservableGauge("telegram.voice.database.available", metric.WithDescription("Whether the most recent voice queue stats query succeeded (0 or 1)"))
	if err != nil {
		return nil, err
	}
	lastSuccess, err := m.Int64ObservableGauge("telegram.voice.database.last_success", metric.WithUnit("s"), metric.WithDescription("Unix timestamp of the last successful queue stats query; zero before first success"))
	if err != nil {
		return nil, err
	}
	var lastSuccessfulQuery atomic.Int64
	return m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var pendingCount, readyCount, cancelledCount int64
		var oldestAge float64
		err := s.Pool.QueryRow(queryCtx, query("stats")).Scan(&pendingCount, &readyCount, &cancelledCount, &oldestAge)
		if err == nil {
			lastSuccessfulQuery.Store(time.Now().Unix())
			o.ObserveInt64(available, 1)
			o.ObserveInt64(pending, pendingCount)
			o.ObserveInt64(ready, readyCount)
			o.ObserveInt64(cancelled, cancelledCount)
			o.ObserveFloat64(age, oldestAge)
		} else {
			// Do not turn a DB failure into a falsely empty queue or export
			// connection errors, identifiers, transcripts or credentials.
			o.ObserveInt64(available, 0)
		}
		o.ObserveInt64(lastSuccess, lastSuccessfulQuery.Load())
		return nil
	}, pending, ready, cancelled, age, available, lastSuccess)
}
