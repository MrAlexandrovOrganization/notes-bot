// Package applog provides the production logger shared by all services.
//
// It follows the ecosystem logging standard: JSON records on stdout with
// time (RFC3339, millisecond precision) / level (DEBUG|INFO|WARN|ERROR,
// matching slog) / msg keys, a "service" attribute on every record, the
// level taken from LOG_LEVEL (debug|info|warn|error, default info) and
// secrets masked with "***" wherever they appear in the output.
package applog

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const masked = "***"

// New creates a consistent production zap logger for all services.
//
// service is attached as the "service" attribute to every record.
// Secrets are masked with "***" in every line written to stdout.
func New(service string, secrets ...string) *zap.Logger {
	return newLogger(os.Stdout, service, levelFromEnv(), secrets...)
}

func newLogger(w io.Writer, service string, level zapcore.Level, secrets ...string) *zap.Logger {
	encoder := zapcore.NewJSONEncoder(zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "",
		CallerKey:      "",
		MessageKey:     "msg",
		StacktraceKey:  "",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     rfc3339MillisTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
	})
	core := zapcore.NewCore(encoder, zapcore.AddSync(&maskWriter{w: w, secrets: nonEmpty(secrets)}), level)
	return zap.New(core, zap.Fields(zap.String("service", service)))
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

func nonEmpty(secrets []string) []string {
	filtered := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// rfc3339MillisTimeEncoder matches the slog JSONHandler time format:
// RFC3339 with millisecond precision (e.g. 2026-08-26T10:00:00.123Z).
func rfc3339MillisTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02T15:04:05.000Z07:00"))
}

// levelFromEnv parses LOG_LEVEL (debug|info|warn|error, case-insensitive;
// default info), mirroring the logx reference implementation.
func levelFromEnv() zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		return zapcore.DebugLevel
	case "", "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// maskWriter masks occurrences of secrets in every complete line before
// writing it through. Line buffering keeps secrets intact even when a JSON
// record is delivered across multiple Write calls.
type maskWriter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets []string
	buf     []byte
}

func (m *maskWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := m.w.Write(maskLine(m.buf[:i+1], m.secrets)); err != nil {
			return 0, err
		}
		m.buf = m.buf[i+1:]
	}
	return len(p), nil
}

// Sync flushes any buffered partial line and propagates to the underlying
// writer when it supports syncing.
func (m *maskWriter) Sync() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buf) > 0 {
		if _, err := m.w.Write(maskLine(m.buf, m.secrets)); err != nil {
			return err
		}
		m.buf = m.buf[:0]
	}
	if s, ok := m.w.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

func maskLine(line []byte, secrets []string) []byte {
	for _, s := range secrets {
		line = bytes.ReplaceAll(line, []byte(s), []byte(masked))
	}
	return line
}
