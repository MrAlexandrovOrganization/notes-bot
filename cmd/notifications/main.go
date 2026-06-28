package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"notes-bot/internal/applog"
	"notes-bot/internal/grpcutil"
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
	if err := cfg.Validate(); err != nil {
		logger.Fatal("invalid config", zap.Error(err))
	}

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
	grpcutil.StartMetricsServer(logger, metricsPort, metricsHandler)

	pool, err := notifications.NewPool(ctx, cfg.DSN())
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()

	if err := notifications.EnsureSchema(ctx, pool); err != nil {
		logger.Fatal("failed to ensure schema", zap.Error(err))
	}

	var esClient *notifications.ESClient
	esClient, err = notifications.NewESClient(cfg.ElasticsearchHost, cfg.ElasticsearchPort)
	if err != nil {
		logger.Warn("failed to connect to elasticsearch, location tracking will only use postgres", zap.Error(err))
		esClient = nil
	} else {
		logger.Info("connected to elasticsearch",
			zap.String("host", cfg.ElasticsearchHost),
			zap.Int("port", cfg.ElasticsearchPort),
		)
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

	// Active location trackers gauge.
	_, err = meter.Int64ObservableGauge("notifications.active.location.trackers",
		metric.WithDescription("Number of users with active location tracking"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			count, err := notifications.CountActiveLocationTracking(ctx, pool)
			if err != nil {
				return err
			}
			o.Observe(count)
			return nil
		}),
	)
	if err != nil {
		logger.Warn("failed to register active location trackers gauge", zap.Error(err))
	}

	scheduler := notifications.NewScheduler(ctx, pool, cfg)
	go scheduler.Run(ctx)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpcutil.NewServer()
	pb.RegisterNotificationsServiceServer(grpcServer, notifications.NewNotificationsServer(pool, cfg, esClient))
	grpcutil.RegisterHealth(grpcServer)

	go func() {
		logger.Info("starting gRPC server", zap.String("port", cfg.GRPCPort))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Error("server stopped", zap.Error(err))
			lis.Close() //nolint:errcheck
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down gracefully...")
	grpcServer.GracefulStop()
}
