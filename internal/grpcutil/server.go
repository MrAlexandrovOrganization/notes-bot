package grpcutil

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// DefaultStopTimeout bounds GracefulStop: one long-running RPC (e.g. an LLM
// call) must not block process shutdown indefinitely.
const DefaultStopTimeout = 15 * time.Second

// NewServer creates a gRPC server with OTel tracing instrumentation.
func NewServer() *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
		)),
	)
}

// RegisterHealth attaches a health server to srv and marks it as SERVING.
// Also enables the gRPC reflection API so tools like grpcurl can discover
// services without a local .proto. gRPC ports are only reachable from the
// docker network, so reflection adds no public exposure.
func RegisterHealth(srv *grpc.Server) {
	hs := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, hs)
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	reflection.Register(srv)
}

// StartMetricsServer starts an HTTP server exposing metricsHandler at /metrics.
func StartMetricsServer(logger *zap.Logger, port string, metricsHandler http.Handler) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler)
		logger.Info("starting metrics server", zap.String("port", port))
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			logger.Error("metrics server stopped", zap.Error(err))
		}
	}()
}

// StopWithTimeout gracefully stops srv. If in-flight RPCs do not finish within
// timeout, the server is force-stopped so process shutdown cannot hang forever.
func StopWithTimeout(logger *zap.Logger, srv *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warn("graceful stop timed out, forcing stop")
		srv.Stop()
	}
}
