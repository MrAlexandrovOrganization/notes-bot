package clients

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Long LLM calls get their own bounded capacity. They must not consume every
// Telegram update slot or make unrelated note operations wait for search.
func searchGuard(probeTimeout, callTimeout time.Duration, capacity int) grpc.UnaryClientInterceptor {
	slots := make(chan struct{}, capacity)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if strings.HasPrefix(method, "/grpc.health.v1.Health/") {
			return invoke(ctx, method, req, reply, cc, opts...)
		}
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			return status.Error(codes.ResourceExhausted, "search is busy; retry later")
		}

		probeCtx, cancelProbe := context.WithTimeout(ctx, probeTimeout)
		health, err := grpc_health_v1.NewHealthClient(cc).Check(probeCtx, &grpc_health_v1.HealthCheckRequest{})
		cancelProbe()
		if err != nil || health.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			if ctx.Err() != nil {
				return status.FromContextError(ctx.Err()).Err()
			}
			return status.Error(codes.Unavailable, "search is temporarily unavailable")
		}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		return invoke(callCtx, method, req, reply, cc, opts...)
	}
}
