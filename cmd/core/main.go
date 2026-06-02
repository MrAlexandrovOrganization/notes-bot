package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"notes-bot/core"
	"notes-bot/internal/grpcutil"
	"notes-bot/internal/telemetry"
	pb "notes-bot/proto/notes"
)

func main() {
	logger := core.Logger

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}

	core.GetConfig(context.Background())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "core")
	if err != nil {
		logger.Fatal("failed to init tracer", zap.Error(err))
	}
	defer shutdown(context.Background()) //nolint:errcheck

	metricsHandler, metricsShutdown, err := telemetry.InitMetrics()
	if err != nil {
		logger.Fatal("failed to init metrics", zap.Error(err))
	}
	defer metricsShutdown()

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9100"
	}
	grpcutil.StartMetricsServer(logger, metricsPort, metricsHandler)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", port))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpcutil.NewServer()

	notesServer := core.NewDefaultNotesServer()
	pb.RegisterNotesServiceServer(grpcServer, notesServer)
	grpcutil.RegisterHealth(grpcServer)

	go func() {
		logger.Info("starting gRPC server", zap.String("port", port))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("server stopped", zap.Error(err))
			lis.Close() //nolint:errcheck
		}
	}()

	go notesServer.LoadHistoricalRatings(ctx)

	<-ctx.Done()
	logger.Info("shutting down gracefully...")
	grpcServer.GracefulStop()
}
