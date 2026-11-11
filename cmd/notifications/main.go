package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"notes-bot/internal/applog"
	"notes-bot/internal/georeminders"
	"notes-bot/internal/grpcutil"
	"notes-bot/internal/telemetry"
	"notes-bot/notifications"
	geopb "notes-bot/proto/georeminders/v1"
	pb "notes-bot/proto/notifications"
)

var logger *zap.Logger

func init() {
	logger = applog.New("notes-bot-notifications")
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
	geoConfig, err := georeminders.LoadConfig()
	if err != nil {
		logger.Fatal("invalid geofence configuration")
	}
	geoStore := &georeminders.Store{Pool: pool}
	if len(os.Args) > 1 && os.Args[1] == "migrate-geo" {
		migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := geoStore.Migrate(migrationCtx); err != nil {
			logger.Fatal("geofence migration failed")
		}
		return
	}

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
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		scheduler.Run(ctx)
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpcutil.NewServer(logger)
	geoDone := make(chan struct{})
	if geoConfig.Address != "" {
		if err := geoStore.Check(ctx); err != nil {
			logger.Fatal("geofence schema unavailable; run make migrate-geo")
		}
		if err := geoStore.RegisterMetrics(); err != nil {
			logger.Warn("geofence metrics unavailable")
		}
		geoServer, conn, err := georeminders.New(geoStore, geoConfig)
		if err != nil {
			logger.Fatal("geofence client unavailable")
		}
		defer conn.Close()
		geopb.RegisterGeoRemindersServiceServer(grpcServer, geoServer)
		go func() {
			defer close(geoDone)
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				if err := geoServer.Poll(pollCtx); err != nil && ctx.Err() == nil {
					logger.Warn("geofence event polling failed")
				}
				cancel()
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	} else {
		close(geoDone)
	}
	pb.RegisterNotificationsServiceServer(grpcServer, notifications.NewNotificationsServer(pool, cfg))
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
	grpcutil.StopWithTimeout(logger, grpcServer, grpcutil.DefaultStopTimeout)
	// Wait for the scheduler to flush in-flight Kafka publishes and release
	// its resources before the deferred pool.Close() runs.
	<-schedulerDone
	<-geoDone
}
