package search

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type searchMetrics struct {
	indexFilesSeen    metric.Int64Counter
	indexFilesAdded   metric.Int64Counter
	indexFilesUpdated metric.Int64Counter
	indexFilesDeleted metric.Int64Counter
	indexFilesTouched metric.Int64Counter
	indexErrors       metric.Int64Counter
	embedCalls        metric.Int64Counter
	searchRequests    metric.Int64Counter
	syncDuration      metric.Float64Histogram
	rpcRequests       metric.Int64Counter
}

func newSearchMetrics() *searchMetrics {
	meter := otel.GetMeterProvider().Meter("search")

	indexFilesSeen, _ := meter.Int64Counter("search.index.files.seen",
		metric.WithDescription("Total files visited during sync"))
	indexFilesAdded, _ := meter.Int64Counter("search.index.files.added",
		metric.WithDescription("Notes inserted during sync"))
	indexFilesUpdated, _ := meter.Int64Counter("search.index.files.updated",
		metric.WithDescription("Notes whose content changed and was re-stored"))
	indexFilesDeleted, _ := meter.Int64Counter("search.index.files.deleted",
		metric.WithDescription("Notes removed because the source file disappeared"))
	indexFilesTouched, _ := meter.Int64Counter("search.index.files.touched",
		metric.WithDescription("Notes whose hash matched; only mtime/size refreshed"))
	indexErrors, _ := meter.Int64Counter("search.index.errors",
		metric.WithDescription("Total errors during sync"))
	embedCalls, _ := meter.Int64Counter("search.embed.calls",
		metric.WithDescription("Total embedding API calls"))
	searchRequests, _ := meter.Int64Counter("search.requests",
		metric.WithDescription("Search RPC requests by kind and status"))
	syncDuration, _ := meter.Float64Histogram("search.sync.duration",
		metric.WithDescription("Duration of a SyncOnce pass"),
		metric.WithUnit("s"))
	rpcRequests, _ := meter.Int64Counter("search.rpc.requests",
		metric.WithDescription("Total gRPC requests by method and status"))

	return &searchMetrics{
		indexFilesSeen:    indexFilesSeen,
		indexFilesAdded:   indexFilesAdded,
		indexFilesUpdated: indexFilesUpdated,
		indexFilesDeleted: indexFilesDeleted,
		indexFilesTouched: indexFilesTouched,
		indexErrors:       indexErrors,
		embedCalls:        embedCalls,
		searchRequests:    searchRequests,
		syncDuration:      syncDuration,
		rpcRequests:       rpcRequests,
	}
}

func (m *searchMetrics) recordSync(ctx context.Context, s SyncStats, took time.Duration) {
	if m == nil {
		return
	}
	m.indexFilesSeen.Add(ctx, int64(s.Seen))
	m.indexFilesAdded.Add(ctx, int64(s.Added))
	m.indexFilesUpdated.Add(ctx, int64(s.Updated))
	m.indexFilesTouched.Add(ctx, int64(s.Touched))
	m.indexFilesDeleted.Add(ctx, int64(s.Deleted))
	m.indexErrors.Add(ctx, int64(s.Errors))
	m.syncDuration.Record(ctx, took.Seconds())
}

func (m *searchMetrics) recordRPC(ctx context.Context, method string, err *error) {
	if m == nil {
		return
	}
	st := "ok"
	if *err != nil {
		st = "error"
	}
	m.rpcRequests.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("method", method),
			attribute.String("status", st),
		),
	)
}

func (m *searchMetrics) recordSearch(ctx context.Context, kind string, err *error) {
	if m == nil {
		return
	}
	st := "ok"
	if *err != nil {
		st = "error"
	}
	m.searchRequests.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("kind", kind),
			attribute.String("status", st),
		),
	)
}
