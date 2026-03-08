package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"notes_bot/notifications"
	pb "notes_bot/proto/notifications"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

var logger *zap.Logger

func init() {
	logger = zap.Must(zap.NewProduction())
}

func main() {
	cfg := notifications.LoadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := notifications.NewPool(ctx, cfg.DSN())
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()

	if err := notifications.EnsureSchema(ctx, pool); err != nil {
		logger.Fatal("failed to ensure schema", zap.Error(err))
	}

	scheduler := notifications.NewScheduler(pool, cfg)
	go scheduler.Run(ctx)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpc.NewServer()
	pb.RegisterNotificationsServiceServer(grpcServer, notifications.NewNotificationsServer(pool, cfg))

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	go func() {
		logger.Info("starting gRPC server", zap.String("port", cfg.GRPCPort))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("server stopped", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully...")
	grpcServer.GracefulStop()
}
