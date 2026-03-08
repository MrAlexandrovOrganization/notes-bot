package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"notes_bot/core"
	pb "notes_bot/proto/notes"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	core.GetConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		core.Logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpc.NewServer()

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
