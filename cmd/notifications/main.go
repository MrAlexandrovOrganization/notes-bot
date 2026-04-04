package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	"notes-bot/notifications"
	pb "notes-bot/proto/notifications"
)

var logger *zap.Logger

func init() {
	logger = applog.New()
}

func main() {
	notifications.SetLogger(logger)

	cfg := notifications.LoadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "notifications")
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
		metricsPort = "9101"
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler)
		logger.Info("starting metrics server", zap.String("port", metricsPort))
		if err := http.ListenAndServe(":"+metricsPort, mux); err != nil {
			logger.Error("metrics server stopped", zap.Error(err))
		}
	}()

	pool, err := notifications.NewPool(ctx, cfg.DSN())
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()

	if err := notifications.EnsureSchema(ctx, pool); err != nil {
		logger.Fatal("failed to ensure schema", zap.Error(err))
	}

	// Active reminders gauge — queried from DB on each Prometheus scrape.
	meter := otel.GetMeterProvider().Meter("notifications")
	_, err = meter.Int64ObservableGauge("notifications.active.reminders",
		metric.WithDescription("Number of active reminders"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			count, err := notifications.CountActiveReminders(ctx, pool)
			if err != nil {
				return err
			}
			o.Observe(count)
			return nil
		}),
	)
	if err != nil {
		logger.Warn("failed to register active reminders gauge", zap.Error(err))
	}

	scheduler := notifications.NewScheduler(ctx, pool, cfg)
	go scheduler.Run(ctx)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
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
