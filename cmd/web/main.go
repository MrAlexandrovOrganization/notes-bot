package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/config"
	"notes-bot/frontends/web/webapp"
	"notes-bot/internal/applog"
	"notes-bot/internal/grpcutil"
	"notes-bot/internal/telemetry"
)

func main() {
	logger := applog.New("notes-bot-web",
		os.Getenv("WEB_PASSWORD"),
		os.Getenv("WEB_SESSION_SECRET"),
	)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatal("invalid config", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracer(ctx, "web")
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
		metricsPort = "9105"
	}
	grpcutil.StartMetricsServer(logger, metricsPort, metricsHandler)

	coreClient, err := clients.NewCoreClient(cfg.CoreGRPCHost, cfg.CoreGRPCPort)
	if err != nil {
		logger.Fatal("failed to create core client", zap.Error(err))
	}
	defer coreClient.Close()

	notifClient, err := clients.NewNotificationsClient(cfg.NotificationsGRPCHost, cfg.NotificationsGRPCPort)
	if err != nil {
		logger.Fatal("failed to create notifications client", zap.Error(err))
	}
	defer notifClient.Close()

	searchClient, err := clients.NewSearchClient(cfg.SearchGRPCHost, cfg.SearchGRPCPort)
	if err != nil {
		logger.Fatal("failed to create search client", zap.Error(err))
	}
	defer searchClient.Close()

	llmClient := clients.NewLLMClient(cfg.LLMHost, cfg.LLMPort, cfg.LLMModel)

	app := &webapp.App{
		Cfg:           cfg,
		Core:          coreClient,
		Notifications: notifClient,
		Search:        searchClient,
		LLM:           llmClient,
		Logger:        logger,
	}

	srv := &http.Server{
		Addr:         cfg.WebListenAddr,
		Handler:      otelhttp.NewHandler(app.NewRouter(), "web"),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute,
	}

	go func() {
		logger.Info("web frontend started", zap.String("addr", cfg.WebListenAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("http server error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down web frontend")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http server shutdown error", zap.Error(err))
	}
}
