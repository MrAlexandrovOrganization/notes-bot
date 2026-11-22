package main

import (
	"context"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"notes-bot/internal/applog"
	"notes-bot/internal/english"
	"notes-bot/internal/env"
	"notes-bot/internal/grpcutil"
	pb "notes-bot/proto/english/v1"
	notes "notes-bot/proto/notes"
)

func databaseURL() string {
	if dsn := os.Getenv("ENGLISH_DATABASE_URL"); dsn != "" {
		return dsn
	}
	password := os.Getenv("ENGLISH_DB_PASSWORD")
	if password == "" {
		return ""
	}
	dsn := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(env.Str("ENGLISH_DB_USER", "english"), password),
		Host:   net.JoinHostPort(env.Str("ENGLISH_DB_HOST", "localhost"), env.Str("ENGLISH_DB_PORT", "5432")),
		Path:   "/" + env.Str("ENGLISH_DB_NAME", "english"),
		// This configuration is for the private Compose network. Use a full
		// ENGLISH_DATABASE_URL to specify TLS on remote PostgreSQL.
		RawQuery: "sslmode=disable",
	}
	return dsn.String()
}

func main() {
	logger := applog.New("notes-bot-english")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := databaseURL()
	if dsn == "" {
		logger.Fatal("English database configuration is required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		logger.Fatal("invalid English database configuration")
	}
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		logger.Fatal("English database unavailable")
	}
	defer pool.Close()
	store := english.Store{Pool: pool}
	conn, err := grpcutil.Dial(env.Str("CORE_GRPC_HOST", "localhost"), env.Str("CORE_GRPC_PORT", "50051"))
	if err != nil {
		logger.Fatal("invalid Notes endpoint")
	}
	defer conn.Close()
	dictionary := english.Dictionary{Notes: notes.NewNotesServiceClient(conn)}

	startup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if len(os.Args) == 2 && os.Args[1] == "migrate" {
		if err = store.Migrate(startup); err != nil {
			logger.Fatal("English schema migration failed")
		}
		if err = dictionary.Migrate(startup); err != nil {
			logger.Fatal("English dictionary migration failed")
		}
		path := os.Getenv("ENGLISH_LEGACY_JOURNAL")
		if path == "" {
			logger.Fatal("ENGLISH_LEGACY_JOURNAL must point to the legacy journal location")
		}
		if err = store.ImportLegacy(startup, path); err != nil {
			logger.Fatal("English review history migration failed")
		}
		logger.Info("English migration complete")
		return
	}
	if len(os.Args) != 1 {
		logger.Fatal("unknown English service command")
	}
	if err = store.Check(startup); err != nil {
		logger.Fatal("English database unavailable or migration required; run make migrate-english")
	}
	cancel()

	listener, err := net.Listen("tcp", env.Str("ENGLISH_LISTEN_ADDR", ":50057"))
	if err != nil {
		logger.Fatal("cannot listen for English gRPC")
	}
	server := grpcutil.NewServer(logger)
	pb.RegisterEnglishServiceServer(server, &english.Server{Dictionary: dictionary, Store: store})
	probe := health.NewServer()
	serviceName := pb.EnglishService_ServiceDesc.ServiceName
	probe.SetServingStatus(serviceName, healthpb.HealthCheckResponse_NOT_SERVING)
	healthpb.RegisterHealthServer(server, probe)
	reflection.Register(server)
	go publishReadiness(ctx, probe, serviceName, pool, healthpb.NewHealthClient(conn))

	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
		probe.Shutdown()
		grpcutil.StopWithTimeout(logger, server, grpcutil.DefaultStopTimeout)
	case err := <-serveError:
		if err != nil {
			logger.Fatal("English gRPC server failed")
		}
	}
}

func publishReadiness(ctx context.Context, probe *health.Server, service string, db *pgxpool.Pool, notes healthpb.HealthClient) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, 2*time.Second)
		result := healthpb.HealthCheckResponse_NOT_SERVING
		if db.Ping(check) == nil {
			response, err := notes.Check(check, &healthpb.HealthCheckRequest{})
			if err == nil {
				result = response.GetStatus()
			}
		}
		cancel()
		probe.SetServingStatus(service, result)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
