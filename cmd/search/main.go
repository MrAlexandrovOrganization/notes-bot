package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

	if err := registerIndexGauges(pool, cfg); err != nil {
		logger.Warn("failed to register index gauges", zap.Error(err))
	}

	var embedder *search.Embedder
	var profileExtractor *search.ProfileExtractor
	var agent *search.NotesAgent
	if cfg.EnableEmbeddings {
		embedder = search.NewEmbedder(cfg.LLMHost, cfg.LLMPort, cfg.EmbedModel, cfg.EmbedDim)
	}
	if cfg.EnableProfiles {
		profileChat := search.NewChatClient(cfg.LLMHost, cfg.LLMPort, cfg.ProfileModel)
		profileExtractor = search.NewProfileExtractor(profileChat, cfg.ProfileModel)
		agentChat := search.NewChatClient(cfg.LLMHost, cfg.LLMPort, cfg.AgentModel)
		agent = search.NewNotesAgent(pool, embedder, agentChat, metrics, cfg.AgentMaxSteps)
	}

	indexer := search.NewIndexer(cfg, pool, metrics, embedder, profileExtractor)
	scheduler := search.NewScheduler(indexer, cfg)
	go scheduler.Run(ctx)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.GRPCPort))
	if err != nil {
		logger.Fatal("failed to listen", zap.Error(err))
	}

	grpcServer := grpcutil.NewServer()
	pb.RegisterSearchServiceServer(grpcServer, search.NewSearchServer(pool, cfg, indexer, metrics, embedder, agent))
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

func registerIndexGauges(pool *pgxpool.Pool, cfg *search.Config) error {
	meter := otel.GetMeterProvider().Meter("search")
	notesTotal, err := meter.Int64ObservableGauge("search.notes.total",
		metric.WithDescription("Total notes known to the search index"))
	if err != nil {
		return err
	}
	if !cfg.EnableEmbeddings {
		_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
			count, queryErr := search.CountNotes(ctx, pool)
			if queryErr == nil {
				observer.ObserveInt64(notesTotal, count)
			}
			return queryErr
		}, notesTotal)
		return err
	}

	pendingNotes, err := meter.Int64ObservableGauge("search.notes.pending_index",
		metric.WithDescription("Notes waiting for the configured index version and embedding model"))
	if err != nil {
		return err
	}
	currentNotes, err := meter.Int64ObservableGauge("search.notes.current_index",
		metric.WithDescription("Notes committed to the configured index version and embedding model"))
	if err != nil {
		return err
	}
	progress, err := meter.Float64ObservableGauge("search.index.progress",
		metric.WithDescription("Fraction of notes committed to the configured index, from 0 to 1"),
		metric.WithUnit("1"))
	if err != nil {
		return err
	}
	chunksTotal, err := meter.Int64ObservableGauge("search.chunks.total",
		metric.WithDescription("Total chunk rows, including stale versions during a rolling reindex"))
	if err != nil {
		return err
	}
	chunksCurrent, err := meter.Int64ObservableGauge("search.chunks.current_index",
		metric.WithDescription("Chunk rows written with the configured index version and embedding model"))
	if err != nil {
		return err
	}
	chunksStale, err := meter.Int64ObservableGauge("search.chunks.stale_index",
		metric.WithDescription("Chunk rows not written with the configured index version and embedding model"))
	if err != nil {
		return err
	}
	latestIndexed, err := meter.Int64ObservableGauge("search.index.latest_note_timestamp",
		metric.WithDescription("Unix timestamp of the last note committed to the configured index"),
		metric.WithUnit("s"))
	if err != nil {
		return err
	}
	profilesTotal, err := meter.Int64ObservableGauge("search.profiles.total",
		metric.WithDescription("Total compact note profiles currently stored"))
	if err != nil {
		return err
	}
	profilesPending, err := meter.Int64ObservableGauge("search.profiles.pending",
		metric.WithDescription("Notes waiting for the configured profile version/models"))
	if err != nil {
		return err
	}
	profileProgress, err := meter.Float64ObservableGauge("search.profiles.progress",
		metric.WithDescription("Fraction of notes with a current compact profile"), metric.WithUnit("1"))
	if err != nil {
		return err
	}
	latestProfile, err := meter.Int64ObservableGauge("search.profiles.latest_timestamp",
		metric.WithDescription("Unix timestamp of the latest current profile"), metric.WithUnit("s"))
	if err != nil {
		return err
	}

	instruments := []metric.Observable{
		notesTotal, pendingNotes, currentNotes, progress,
		chunksTotal, chunksCurrent, chunksStale, latestIndexed,
		profilesTotal, profilesPending, profileProgress, latestProfile,
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		snapshot, queryErr := search.ReadIndexMetricsSnapshot(ctx, pool, cfg.EmbedModel, cfg.ProfileModel)
		if queryErr != nil {
			return queryErr
		}
		attrs := metric.WithAttributes(
			attribute.Int("index_version", search.CurrentIndexVersion),
			attribute.String("embedding_model", cfg.EmbedModel),
		)
		current := snapshot.TotalNotes - snapshot.PendingNotes
		staleChunks := snapshot.TotalChunks - snapshot.CurrentChunks
		if staleChunks < 0 {
			staleChunks = 0
		}
		progressValue := 1.0
		if snapshot.TotalNotes > 0 {
			progressValue = float64(current) / float64(snapshot.TotalNotes)
		}

		observer.ObserveInt64(notesTotal, snapshot.TotalNotes)
		observer.ObserveInt64(pendingNotes, snapshot.PendingNotes, attrs)
		observer.ObserveInt64(currentNotes, current, attrs)
		observer.ObserveFloat64(progress, progressValue, attrs)
		observer.ObserveInt64(chunksTotal, snapshot.TotalChunks)
		observer.ObserveInt64(chunksCurrent, snapshot.CurrentChunks, attrs)
		observer.ObserveInt64(chunksStale, staleChunks, attrs)
		observer.ObserveInt64(latestIndexed, snapshot.LatestIndexedUnix, attrs)
		profileAttrs := metric.WithAttributes(
			attribute.Int("profile_version", search.CurrentProfileVersion),
			attribute.String("profile_model", cfg.ProfileModel),
			attribute.String("embedding_model", cfg.EmbedModel),
		)
		profileProgressValue := 1.0
		if snapshot.TotalNotes > 0 {
			profileProgressValue = float64(snapshot.TotalNotes-snapshot.PendingProfiles) / float64(snapshot.TotalNotes)
			profileProgressValue = max(0, min(1, profileProgressValue))
		}
		observer.ObserveInt64(profilesTotal, snapshot.TotalProfiles, profileAttrs)
		observer.ObserveInt64(profilesPending, snapshot.PendingProfiles, profileAttrs)
		observer.ObserveFloat64(profileProgress, profileProgressValue, profileAttrs)
		observer.ObserveInt64(latestProfile, snapshot.LatestProfileUnix, profileAttrs)
		return nil
	}, instruments...)
	return err
}
