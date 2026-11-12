package clients

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/search"
)

type blockedSearch struct {
	pb.UnimplementedSearchServiceServer
	entered chan struct{}
	release chan struct{}
}

func (s *blockedSearch) FindNotes(ctx context.Context, _ *pb.SearchRequest) (*pb.SearchResponse, error) {
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return &pb.SearchResponse{}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func guardedSearchFixture(t *testing.T, serving bool) (*SearchClient, *blockedSearch) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	backend := &blockedSearch{entered: make(chan struct{}, 4), release: make(chan struct{})}
	pb.RegisterSearchServiceServer(server, backend)
	healthServer := health.NewServer()
	state := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if serving {
		state = grpc_health_v1.HealthCheckResponse_SERVING
	}
	healthServer.SetServingStatus("", state)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithChainUnaryInterceptor(searchGuard(time.Second, 2*time.Second, 2)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &SearchClient{conn: conn, stub: pb.NewSearchServiceClient(conn)}, backend
}

func TestSearchNotReadyDoesNotExecuteQueries(t *testing.T) {
	client, backend := guardedSearchFixture(t, false)
	_, err := client.FindNotes(context.Background(), "synthetic", 1, SearchOptions{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("got %v, want Unavailable", err)
	}
	if len(backend.entered) != 0 {
		t.Fatal("an unavailable backend received a search")
	}
}

func TestBlockedSearchHasBoundedCapacityAndReleasesIt(t *testing.T) {
	client, backend := guardedSearchFixture(t, true)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := client.FindNotes(context.Background(), "synthetic", 1, SearchOptions{})
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-backend.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("search did not enter fixture")
		}
	}
	start := time.Now()
	_, err := client.FindNotes(context.Background(), "synthetic", 1, SearchOptions{})
	if status.Code(err) != codes.ResourceExhausted || time.Since(start) > time.Second {
		t.Fatalf("busy search did not fail promptly: %v", err)
	}
	close(backend.release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.FindNotes(context.Background(), "synthetic", 1, SearchOptions{}); err != nil {
		t.Fatalf("capacity did not recover: %v", err)
	}
}

func TestSearchBlackholeHasShortAvailabilityDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// The socket accepts TCP, but never answers HTTP/2 or gRPC.
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithChainUnaryInterceptor(searchGuard(100*time.Millisecond, time.Minute, 2)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &SearchClient{conn: conn, stub: pb.NewSearchServiceClient(conn)}
	start := time.Now()
	_, err = client.FindNotes(context.Background(), "synthetic", 1, SearchOptions{})
	if status.Code(err) != codes.Unavailable || time.Since(start) > time.Second {
		t.Fatalf("blackholed search waited too long: %v", err)
	}
}
