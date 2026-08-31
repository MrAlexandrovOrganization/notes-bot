package applog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func captureLog(t *testing.T, level zapcore.Level, service string, secrets []string, fn func(l *zap.Logger)) map[string]any {
	t.Helper()
	var buf strings.Builder
	logger := newLogger(&buf, service, level, secrets...)
	fn(logger)
	if err := logger.Sync(); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("record does not end with newline: %q", line)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, line)
	}
	return record
}

func TestLoggerFormat(t *testing.T) {
	record := captureLog(t, zapcore.InfoLevel, "notes-bot-test", nil, func(l *zap.Logger) {
		l.Info("hello world", zap.String("key", "value"))
	})

	if got := record["service.name"]; got != "notes-bot-test" {
		t.Errorf("service.name = %v, want notes-bot-test", got)
	}
	if got := record["severity_text"]; got != "INFO" {
		t.Errorf("severity_text = %v, want INFO", got)
	}
	if got := record["severity_number"]; got != float64(9) {
		t.Errorf("severity_number = %v, want 9", got)
	}
	if got := record["body"]; got != "hello world" {
		t.Errorf("body = %v, want hello world", got)
	}
	if got := record["key"]; got != "value" {
		t.Errorf("key = %v, want value", got)
	}
	ts, err := time.Parse("2006-01-02T15:04:05.000Z07:00", record["timestamp"].(string))
	if err != nil {
		t.Errorf("timestamp %v is not RFC3339 with milliseconds: %v", record["timestamp"], err)
	} else if ts.IsZero() {
		t.Error("timestamp is zero")
	}
	for _, unwanted := range []string{"time", "level", "msg", "service", "caller", "stacktrace", "logger", "ts"} {
		if _, ok := record[unwanted]; ok {
			t.Errorf("unexpected key %q in record", unwanted)
		}
	}
}

func TestLoggerMasking(t *testing.T) {
	record := captureLog(t, zapcore.InfoLevel, "notes-bot-test",
		[]string{"supersecret123"}, func(l *zap.Logger) {
			l.Warn("token leaked", zap.String("token", "supersecret123"))
		})

	if msg, _ := record["body"].(string); strings.Contains(msg, "secret") && msg != "*** leaked" {
		t.Errorf("message not masked: %v", record["body"])
	}
	if got := record["token"]; got != "***" {
		t.Errorf("attribute not masked: %v", got)
	}
}

func TestLevelFromEnv(t *testing.T) {
	cases := map[string]zapcore.Level{
		"":        zapcore.InfoLevel,
		"info":    zapcore.InfoLevel,
		"DEBUG":   zapcore.DebugLevel,
		"warn":    zapcore.WarnLevel,
		"warning": zapcore.WarnLevel,
		"error":   zapcore.ErrorLevel,
		"bogus":   zapcore.InfoLevel,
	}
	for env, want := range cases {
		t.Run(env, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", env)
			if got := levelFromEnv(); got != want {
				t.Errorf("levelFromEnv(LOG_LEVEL=%q) = %v, want %v", env, got, want)
			}
		})
	}
}

func TestWithAddsTraceContext(t *testing.T) {
	record := captureLog(t, zapcore.InfoLevel, "notes-bot-test", nil, func(l *zap.Logger) {
		l.Info("no span here")
	})
	if _, ok := record["trace_id"]; ok {
		t.Error("trace_id present without a span")
	}

	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))
	withTrace := captureLog(t, zapcore.InfoLevel, "notes-bot-test", nil, func(l *zap.Logger) {
		With(ctx, l).Info("with trace")
	})
	if got := withTrace["trace_id"]; got != traceID.String() {
		t.Errorf("trace_id = %v, want %s", got, traceID)
	}
	if got := withTrace["span_id"]; got != spanID.String() {
		t.Errorf("span_id = %v, want %s", got, spanID)
	}
	if got := withTrace["trace_flags"]; got != "01" {
		t.Errorf("trace_flags = %v, want 01", got)
	}
}
