package telemetry

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

// InitMetrics creates a Prometheus exporter, registers a global MeterProvider,
// and returns an HTTP handler serving /metrics.
// Returns a shutdown function that must be called on service exit.
// No-op (returns promhttp.Handler over empty registry) if called multiple times
// since the global provider is replaced each time — call once per process.
func InitMetrics() (http.Handler, func(), error) {
	exporter, err := otelprometheus.New()
	if err != nil {
		return nil, nil, err
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	return promhttp.Handler(), func() {
		_ = provider.Shutdown(context.Background())
	}, nil
}
