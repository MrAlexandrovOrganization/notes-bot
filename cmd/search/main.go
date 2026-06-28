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
	pb "notes-bot/proto/search"
	"notes-bot/search"
)

var logger *zap.Logger

func init() {
	logger = applog.New()
}

func main() {
	search.SetLogger(logger)

	cfg := search.LoadConfig()
	if err := cfg.Validate(); err != nil {
		logger.Fatal("invalid config", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "search")
	if err != nil {
		logger.Fatal("failed to init tracer", zap.Error(err))
	}
	defer shutdown(context.Background()) //nolint:errcheck

	metricsHandler, metricsShutdown, err := telemetry.InitMetrics()
	if err != nil {
		logger.Fatal("failed to init metrics", zap.Error(err))
	}
	defer metricsShutdown()
	grpcutil.StartMetricsServer(logger, cfg.MetricsPort, metricsHandler)

	pool, err := search.NewPool(ctx, cfg.DSN())
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()

	if err := search.EnsureSchema(ctx, pool, cfg.EnableEmbeddings, cfg.EmbedDim); err != nil {
		logger.Fatal("failed to ensure schema", zap.Error(err))
	}

	metrics := search.NewMetrics()

	meter := otel.GetMeterProvider().Meter("search")
	_, err = meter.Int64ObservableGauge("search.notes.total",
		metric.WithDescription("Total notes indexed"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			count, err := search.CountNotes(ctx, pool)
			if err != nil {
				return err
			}
			o.Observe(count)
			return nil
		}),
	)
	if err != nil {
		logger.Warn("failed to register notes gauge", zap.Error(err))
	}

	indexer := search.NewIndexer(cfg, pool, metrics)
	scheduler := search.NewScheduler(indexer, cfg)
	go scheduler.Run(ctx)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpcutil.NewServer()
	pb.RegisterSearchServiceServer(grpcServer, search.NewSearchServer(pool, cfg, indexer, metrics))
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
