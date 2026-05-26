package grpcutil

import (
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const DefaultTimeout = 10 * time.Second

// Dial creates a gRPC client connection with tracing, insecure credentials,
// and a default timeout interceptor. Pass extra options for per-client overrides
// (e.g. custom message size limits or a different timeout interceptor).
func Dial(host, port string, extraOpts ...grpc.DialOption) (*grpc.ClientConn, error) {
	base := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(TimeoutInterceptor(DefaultTimeout)),
	}
	return grpc.NewClient(
		fmt.Sprintf("%s:%s", host, port),
		append(base, extraOpts...)...,
	)
}
