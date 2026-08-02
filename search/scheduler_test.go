package search

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type tracingSyncer struct {
	calls chan struct{}
}

func (s *tracingSyncer) SyncOnce(ctx context.Context) (SyncStats, error) {
	_, span := otel.Tracer("search-scheduler-test").Start(ctx, "syncOnce")
	span.End()
	select {
	case s.calls <- struct{}{}:
	default:
	}
	return SyncStats{}, nil
}

func TestSchedulerStartsEachSyncInADistinctTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	syncer := &tracingSyncer{calls: make(chan struct{}, 8)}
	scheduler := newScheduler(syncer, &Config{IndexInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-syncer.calls:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("timed out waiting for scheduled sync")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}

	var syncSpans []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "syncOnce" {
			syncSpans = append(syncSpans, span)
		}
	}
	if len(syncSpans) < 2 {
		t.Fatalf("got %d completed sync spans, want at least 2", len(syncSpans))
	}
	if syncSpans[0].SpanContext().TraceID() == syncSpans[1].SpanContext().TraceID() {
		t.Fatalf("consecutive syncs share trace ID %s", syncSpans[0].SpanContext().TraceID())
	}
}
