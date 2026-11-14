package search

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/english/v1"
)

func tutorResponse(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": `{"source":"Я потратил полчаса","english":"I spent half an hour trying to fix it.","explanation":"spend time doing something","example":"I spent an hour reading.","context":"repair","question":""}`}})
}
func TestEnglishTutorUsesOnlySelectedInputOverGRPC(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body chatRequest
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.Len(t, body.Messages, 2)
		require.NotNil(t, body.Format)
		require.False(t, *body.Think)
		require.Equal(t, "synthetic-model", body.Model)
		require.Contains(t, body.Messages[1].Content, "Выбранная мысль")
		require.NotContains(t, body.Messages[1].Content, "vault")
		tutorResponse(w)
	}))
	defer httpServer.Close()
	tutor := NewTutorServer(&ChatClient{baseURL: httpServer.URL, model: "synthetic-model", http: httpServer.Client()})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())
	pb.RegisterTutorServiceServer(server, tutor)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	client, err := clients.NewSearchClient(host, port)
	require.NoError(t, err)
	defer client.Close()
	result, err := client.PrepareEnglish(t.Context(), "phrase", "Выбранная мысль", "", nil)
	require.NoError(t, err)
	require.Equal(t, "I spent half an hour trying to fix it.", result.English)
	require.Equal(t, "Выбранная мысль", result.Source)
}
func TestEnglishTutorLimitsConcurrencyAndHidesUpstreamErrors(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("synthetic private upstream error"))
	}))
	defer httpServer.Close()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	tutor := NewTutorServer(&ChatClient{baseURL: httpServer.URL, http: httpServer.Client()})
	request := &pb.PrepareRequest{Kind: "word", Input: "gate"}
	errors := make(chan error, 2)
	for range 2 {
		go func() { _, err := tutor.Prepare(context.Background(), request); errors <- err }()
	}
	<-entered
	<-entered
	_, err := tutor.Prepare(t.Context(), request)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	unblock()
	for range 2 {
		err := <-errors
		require.Equal(t, codes.Unavailable, status.Code(err))
		require.NotContains(t, err.Error(), "private upstream")
	}
	_, err = NewTutorServer(nil).Prepare(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, err = tutor.Prepare(t.Context(), &pb.PrepareRequest{Kind: "phrase", Input: "I want coffee."})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = tutor.Prepare(t.Context(), &pb.PrepareRequest{Kind: "phrase", Input: strings.Repeat("x", 1001)})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
