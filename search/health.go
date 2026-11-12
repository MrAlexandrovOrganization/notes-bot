package search

import (
	"context"
	"time"

	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type databasePinger interface {
	Ping(context.Context) error
}

// Search readiness includes its own database. Other Notes services have no
// health dependency on this service or its database.
type HealthServer struct {
	*health.Server
	database databasePinger
}

func NewHealthServer(database databasePinger) *HealthServer {
	server := health.NewServer()
	server.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	return &HealthServer{Server: server, database: database}
}

func (h *HealthServer) Check(ctx context.Context, request *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := h.database.Ping(ctx); err != nil {
		return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, nil
	}
	return h.Server.Check(ctx, request)
}
