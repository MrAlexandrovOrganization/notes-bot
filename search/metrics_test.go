package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSearchMetricsExposeBackfillAndRetrievalSeries(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(previous)
	})

	metrics := newSearchMetrics()
	ctx := context.Background()
	metrics.recordEmbed(ctx, []string{"one", "два"}, time.Second, nil)
	metrics.recordEmbed(ctx, []string{"failed"}, 2*time.Second, errors.New("ollama unavailable"))
	metrics.recordIndexNote(ctx, "backfill", 4, 3*time.Second, nil)
	metrics.recordHybridCandidates(ctx, "dense", 40)
	var searchErr error
	metrics.recordSearch(ctx, "hybrid", time.Now().Add(-time.Second), func() int { return 7 }, &searchErr)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"search.embed.calls":           false,
		"search.embed.inputs":          false,
		"search.embed.input.runes":     false,
		"search.embed.duration":        false,
		"search.index.notes.processed": false,
		"search.index.chunks.embedded": false,
		"search.index.note.duration":   false,
		"search.requests":              false,
		"search.duration":              false,
		"search.results":               false,
		"search.hybrid.candidates":     false,
	}
	for _, scope := range collected.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if _, ok := want[measurement.Name]; ok {
				want[measurement.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric %q was not collected", name)
		}
	}
}

func TestSearchMetricsPrometheusNamesMatchDashboard(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		t.Fatal(err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(previous)
	})

	metrics := newSearchMetrics()
	ctx := context.Background()
	metrics.recordEmbed(ctx, []string{"chunk"}, time.Second, nil)
	metrics.recordIndexNote(ctx, "backfill", 1, time.Second, nil)
	metrics.recordHybridCandidates(ctx, "dense", 1)
	var searchErr error
	metrics.recordSearch(ctx, "hybrid", time.Now(), func() int { return 1 }, &searchErr)

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(families))
	for _, family := range families {
		got[family.GetName()] = true
	}
	for _, want := range []string{
		"search_embed_calls_total",
		"search_embed_inputs_total",
		"search_embed_duration_seconds",
		"search_index_notes_processed_total",
		"search_index_chunks_embedded_total",
		"search_index_note_duration_seconds",
		"search_requests_total",
		"search_duration_seconds",
		"search_results",
		"search_hybrid_candidates",
	} {
		if !got[want] {
			t.Errorf("Prometheus metric %q was not exported; got %v", want, got)
		}
	}
}
