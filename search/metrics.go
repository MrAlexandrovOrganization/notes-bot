package search

import (
	"context"
	"time"
	"unicode/utf8"

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
	embedInputs       metric.Int64Counter
	embedInputRunes   metric.Int64Counter
	embedDuration     metric.Float64Histogram
	indexNotes        metric.Int64Counter
	indexChunks       metric.Int64Counter
	indexProfiles     metric.Int64Counter
	indexNoteDuration metric.Float64Histogram
	searchRequests    metric.Int64Counter
	searchDuration    metric.Float64Histogram
	searchResults     metric.Int64Histogram
	hybridCandidates  metric.Int64Histogram
	syncDuration      metric.Float64Histogram
	rpcRequests       metric.Int64Counter
	agentRequests     metric.Int64Counter
	agentDuration     metric.Float64Histogram
	agentSteps        metric.Int64Histogram
	agentEvidence     metric.Int64Histogram
	agentExhaustive   metric.Int64Histogram
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
		metric.WithDescription("Total embedding API calls by status"))
	embedInputs, _ := meter.Int64Counter("search.embed.inputs",
		metric.WithDescription("Total texts sent to the embedding API by status"))
	embedInputRunes, _ := meter.Int64Counter("search.embed.input.runes",
		metric.WithDescription("Total Unicode code points sent to the embedding API by status"))
	embedDuration, _ := meter.Float64Histogram("search.embed.duration",
		metric.WithDescription("Embedding API request duration by status"),
		metric.WithUnit("s"))
	indexNotes, _ := meter.Int64Counter("search.index.notes.processed",
		metric.WithDescription("Notes processed by the chunk index by source and status"))
	indexChunks, _ := meter.Int64Counter("search.index.chunks.embedded",
		metric.WithDescription("Chunks successfully embedded by index source"))
	indexProfiles, _ := meter.Int64Counter("search.index.profiles.indexed",
		metric.WithDescription("Compact note profiles successfully extracted and embedded"))
	indexNoteDuration, _ := meter.Float64Histogram("search.index.note.duration",
		metric.WithDescription("End-to-end duration of indexing one note by source and status"),
		metric.WithUnit("s"))
	searchRequests, _ := meter.Int64Counter("search.requests",
		metric.WithDescription("Search RPC requests by kind and status"))
	searchDuration, _ := meter.Float64Histogram("search.duration",
		metric.WithDescription("Search RPC duration by kind and status"),
		metric.WithUnit("s"))
	searchResults, _ := meter.Int64Histogram("search.results",
		metric.WithDescription("Number of hits returned by successful search RPCs by kind"))
	hybridCandidates, _ := meter.Int64Histogram("search.hybrid.candidates",
		metric.WithDescription("Hybrid retrieval candidates by pipeline stage"))
	syncDuration, _ := meter.Float64Histogram("search.sync.duration",
		metric.WithDescription("Duration of a SyncOnce pass"),
		metric.WithUnit("s"))
	rpcRequests, _ := meter.Int64Counter("search.rpc.requests",
		metric.WithDescription("Total gRPC requests by method and status"))
	agentRequests, _ := meter.Int64Counter("search.agent.requests",
		metric.WithDescription("Notes agent requests by status and budget outcome"))
	agentDuration, _ := meter.Float64Histogram("search.agent.duration",
		metric.WithDescription("End-to-end notes agent duration"), metric.WithUnit("s"))
	agentSteps, _ := meter.Int64Histogram("search.agent.steps",
		metric.WithDescription("Retrieval/review rounds used by the notes agent"))
	agentEvidence, _ := meter.Int64Histogram("search.agent.evidence_chunks",
		metric.WithDescription("Distinct raw chunks supplied to final synthesis"))
	agentExhaustive, _ := meter.Int64Histogram("search.agent.exhaustive_matches",
		metric.WithDescription("Coverage of exhaustive FTS tool calls by dimension"))

	return &searchMetrics{
		indexFilesSeen:    indexFilesSeen,
		indexFilesAdded:   indexFilesAdded,
		indexFilesUpdated: indexFilesUpdated,
		indexFilesDeleted: indexFilesDeleted,
		indexFilesTouched: indexFilesTouched,
		indexErrors:       indexErrors,
		embedCalls:        embedCalls,
		embedInputs:       embedInputs,
		embedInputRunes:   embedInputRunes,
		embedDuration:     embedDuration,
		indexNotes:        indexNotes,
		indexChunks:       indexChunks,
		indexProfiles:     indexProfiles,
		indexNoteDuration: indexNoteDuration,
		searchRequests:    searchRequests,
		searchDuration:    searchDuration,
		searchResults:     searchResults,
		hybridCandidates:  hybridCandidates,
		syncDuration:      syncDuration,
		rpcRequests:       rpcRequests,
		agentRequests:     agentRequests,
		agentDuration:     agentDuration,
		agentSteps:        agentSteps,
		agentEvidence:     agentEvidence,
		agentExhaustive:   agentExhaustive,
	}
}

func (m *searchMetrics) recordAgentExhaustive(ctx context.Context, chunks, notes, dates int) {
	if m == nil {
		return
	}
	for dimension, value := range map[string]int{"chunks": chunks, "notes": notes, "dates": dates} {
		m.agentExhaustive.Record(ctx, int64(value),
			metric.WithAttributes(attribute.String("dimension", dimension)))
	}
}

func (m *searchMetrics) recordAgent(ctx context.Context, took time.Duration, steps, evidence int, exhausted bool, err error) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("status", metricStatus(err)),
		attribute.Bool("budget_exhausted", exhausted),
	)
	m.agentRequests.Add(ctx, 1, attrs)
	m.agentDuration.Record(ctx, took.Seconds(), attrs)
	if err == nil {
		m.agentSteps.Record(ctx, int64(steps))
		m.agentEvidence.Record(ctx, int64(evidence))
	}
}

func metricStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func (m *searchMetrics) recordEmbed(ctx context.Context, inputs []string, took time.Duration, err error) {
	if m == nil || len(inputs) == 0 {
		return
	}
	status := metricStatus(err)
	attrs := metric.WithAttributes(attribute.String("status", status))
	var runes int64
	for _, input := range inputs {
		runes += int64(utf8.RuneCountInString(input))
	}
	m.embedCalls.Add(ctx, 1, attrs)
	m.embedInputs.Add(ctx, int64(len(inputs)), attrs)
	m.embedInputRunes.Add(ctx, runes, attrs)
	m.embedDuration.Record(ctx, took.Seconds(), attrs)
}

func (m *searchMetrics) recordIndexNote(ctx context.Context, source string, embedded int, took time.Duration, err error) {
	if m == nil {
		return
	}
	status := metricStatus(err)
	attrs := metric.WithAttributes(
		attribute.String("source", source),
		attribute.String("status", status),
	)
	m.indexNotes.Add(ctx, 1, attrs)
	m.indexNoteDuration.Record(ctx, took.Seconds(), attrs)
	if embedded > 0 {
		m.indexChunks.Add(ctx, int64(embedded),
			metric.WithAttributes(attribute.String("source", source)))
	}
}

func (m *searchMetrics) recordHybridCandidates(ctx context.Context, stage string, count int) {
	if m == nil {
		return
	}
	m.hybridCandidates.Record(ctx, int64(count),
		metric.WithAttributes(attribute.String("stage", stage)))
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
	m.indexProfiles.Add(ctx, int64(s.Profiled))
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

func (m *searchMetrics) recordSearch(ctx context.Context, kind string, started time.Time, resultCount func() int, err *error) {
	if m == nil {
		return
	}
	st := metricStatus(*err)
	attrs := metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("status", st),
	)
	m.searchRequests.Add(ctx, 1,
		attrs,
	)
	m.searchDuration.Record(ctx, time.Since(started).Seconds(), attrs)
	if *err == nil && resultCount != nil {
		m.searchResults.Record(ctx, int64(resultCount()),
			metric.WithAttributes(attribute.String("kind", kind)))
	}
}
