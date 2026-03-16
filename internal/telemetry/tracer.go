package telemetry

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitTracer initialises a global TracerProvider that exports to Jaeger via OTLP gRPC.
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, the function is a no-op and returns a
// no-op shutdown function so callers don't need special-case logic.
//
// Returns a shutdown function that must be called on service exit.
func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// StartSpan starts a new span, automatically resolving the tracer name and span name
// from the caller's function via runtime.Caller — no magic strings needed.
//
// Usage:
//
//	ctx, span := telemetry.StartSpan(ctx)
//	defer span.End()
//
// Optional attributes can be passed inline or added later via span.SetAttributes.
func StartSpan(ctx context.Context, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	pkg, name := callerInfo(1)
	var opts []trace.SpanStartOption
	if len(attrs) > 0 {
		opts = append(opts, trace.WithAttributes(attrs...))
	}
	return otel.Tracer(pkg).Start(ctx, name, opts...)
}

// callerInfo resolves the short package name and function name of the caller.
// skip=1 means "skip callerInfo itself, return its caller's caller".
func callerInfo(skip int) (pkg, name string) {
	pc, _, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return "app", "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "app", "unknown"
	}
	full := fn.Name()
	// full examples:
	//   "notes_bot/core.(*NotesServer).AppendToNote"
	//   "notes_bot/frontends/telegram/tghandlers.(*App).HandleTextMessage"
	//   "notes_bot/frontends/telegram/tghandlers.handleRatingInput"

	// Package: path before the first dot, then last path segment.
	if dot := strings.Index(full, "."); dot >= 0 {
		pkgPath := full[:dot]
		if slash := strings.LastIndex(pkgPath, "/"); slash >= 0 {
			pkg = pkgPath[slash+1:]
		} else {
			pkg = pkgPath
		}
	}

	// Function name: everything after the last dot; strip "-fm" (method value suffix).
	if dot := strings.LastIndex(full, "."); dot >= 0 {
		name = full[dot+1:]
	} else {
		name = full
	}
	name = strings.TrimSuffix(name, "-fm")

	return pkg, name
}
