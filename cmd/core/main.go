package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"notes_bot/core"
	"notes_bot/internal/telemetry"
	pb "notes_bot/proto/notes"
)

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	core.GetConfig(context.Background())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "core")
	if err != nil {
		core.Logger.Fatal("failed to init tracer", zap.Error(err))
	}
	defer shutdown(context.Background()) //nolint:errcheck

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		core.Logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)

	pb.RegisterNotesServiceServer(grpcServer, core.NewNotesServer())

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	go func() {
		core.Logger.Info("starting gRPC server", zap.String("port", port))
		if err := grpcServer.Serve(lis); err != nil {
			core.Logger.Error("server stopped", zap.Error(err))
		}
	}()

	<-ctx.Done()
	core.Logger.Info("shutting down gracefully...")
	grpcServer.GracefulStop()
}
