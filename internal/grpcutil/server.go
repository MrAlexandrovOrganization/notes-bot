package grpcutil

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// NewServer creates a gRPC server with OTel tracing instrumentation.
func NewServer() *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
}

// RegisterHealth attaches a health server to srv and marks it as SERVING.
func RegisterHealth(srv *grpc.Server) {
	hs := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, hs)
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
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
