package applog

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// New creates a consistent production zap logger for all services.
func New() *zap.Logger {
	return zap.Must(zap.NewProduction())
}

// With returns a child logger enriched with trace_id and span_id fields
// extracted from the OpenTelemetry span in ctx. If there is no valid span,
// the original logger is returned unchanged.
func With(ctx context.Context, l *zap.Logger) *zap.Logger {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return l
	}
	return l.With(
		zap.String("trace_id", sc.TraceID().String()),
		zap.String("span_id", sc.SpanID().String()),
	)
}
