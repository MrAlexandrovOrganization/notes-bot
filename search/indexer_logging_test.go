package search

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"notes-bot/internal/applog"
)

func TestLogSyncSummary(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	})
	for _, scheduled := range []bool{false, true} {
		origin := "api"
		if scheduled {
			origin = "scheduled"
		}
		for _, tc := range []struct {
			name  string
			stats SyncStats
		}{
			{"empty", SyncStats{}},
			{"seen only", SyncStats{Seen: 10}},
			{"added", SyncStats{Added: 1}},
			{"updated", SyncStats{Updated: 1}},
			{"touched", SyncStats{Touched: 1}},
			{"deleted", SyncStats{Deleted: 1}},
			{"embedded", SyncStats{Embedded: 1}},
			{"profiled", SyncStats{Profiled: 1}},
			{"errors", SyncStats{Errors: 1}},
			{"changes and errors", SyncStats{Added: 1, Errors: 1}},
		} {
			t.Run(origin+"/"+tc.name, func(t *testing.T) {
				ctx := trace.ContextWithSpanContext(context.Background(), sc)
				if scheduled {
					ctx = context.WithValue(ctx, scheduledSyncKey{}, true)
				}
				wantLevel := zap.InfoLevel
				if tc.stats.Errors > 0 {
					wantLevel = zap.WarnLevel
				} else if scheduled && (tc.name == "empty" || tc.name == "seen only") {
					wantLevel = zap.DebugLevel
				}
				for _, threshold := range []zap.AtomicLevel{zap.NewAtomicLevelAt(zap.DebugLevel), zap.NewAtomicLevelAt(zap.InfoLevel)} {
					core, logs := observer.New(threshold)
					logSyncSummary(ctx, applog.With(ctx, zap.New(core)), tc.stats, time.Second)
					if wantLevel < threshold.Level() {
						require.Zero(t, logs.Len())
						continue
					}
					require.Equal(t, 1, logs.Len())
					entry := logs.All()[0]
					require.Equal(t, wantLevel, entry.Level)
					require.Equal(t, "sync done", entry.Message)
					require.Equal(t, map[string]any{
						"seen": int64(tc.stats.Seen), "added": int64(tc.stats.Added),
						"updated": int64(tc.stats.Updated), "touched": int64(tc.stats.Touched),
						"deleted": int64(tc.stats.Deleted), "embedded": int64(tc.stats.Embedded),
						"profiled": int64(tc.stats.Profiled), "errors": int64(tc.stats.Errors),
						"took": time.Second, "trace_id": sc.TraceID().String(),
						"span_id": sc.SpanID().String(), "trace_flags": sc.TraceFlags().String(),
					}, entry.ContextMap())
				}
			})
		}
	}
}
