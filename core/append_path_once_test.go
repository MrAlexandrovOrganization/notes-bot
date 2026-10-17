package core

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "notes-bot/proto/notes"
)

func TestAppendByPathOnceRetriesAndRejectsInvalidDestinations(t *testing.T) {
	daily := setupNotesEnv(t)
	root := filepath.Dir(daily)
	relpath := "Films/Первый.md"
	path := filepath.Join(root, relpath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("Existing review"), 0640))
	store := &realNoteStore{}
	text, id := "Synthetic review 🌍\nsecond line", "voice:film"
	require.NoError(t, store.AppendByPathOnce(t.Context(), relpath, text, id))
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, store.AppendByPathOnce(t.Context(), "Films/./Первый.md", text, id))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Contains(t, string(first), "Existing review\n"+text)
	require.Equal(t, 1, strings.Count(string(first), text))
	require.ErrorIs(t, store.AppendByPathOnce(t.Context(), relpath, "different text", id), ErrOperationConflict)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0640), info.Mode().Perm())
	for _, invalidID := range []string{"", "bad id", "../path"} {
		require.ErrorIs(t, store.AppendByPathOnce(t.Context(), relpath, text, invalidID), ErrInvalidOperationID)
	}
	for _, invalidPath := range []string{"", "../outside.md", root, "Films", "Films/Absent.md"} {
		require.Error(t, store.AppendByPathOnce(t.Context(), invalidPath, text, "other"))
	}
	require.NoFileExists(t, filepath.Join(root, "Films/Absent.md"))
	require.Error(t, store.AppendByPathOnce(t.Context(), relpath, appendMarkerPrefix+"forged -->", "other"))
	out := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(out, []byte("unchanged"), 0600))
	require.NoError(t, os.Symlink(out, filepath.Join(root, "Films/Link.md")))
	require.Error(t, store.AppendByPathOnce(t.Context(), "Films/Link.md", text, "other"))
	outside, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(outside))
	// The path writer shares the lock with ordinary appends and task writers.
	unlock, err := lockFile(t.Context(), path)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, store.AppendByPathOnce(ctx, relpath, "blocked", "blocked"), context.DeadlineExceeded)
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, first, unchanged)
}

func TestAppendByPathOnceLostGRPCResponseDoesNotDuplicate(t *testing.T) {
	daily := setupNotesEnv(t)
	relpath := "Film.md"
	path := filepath.Join(filepath.Dir(daily), relpath)
	require.NoError(t, os.WriteFile(path, []byte("Original\n"), 0644))
	listener := bufconn.Listen(1024 * 1024)
	var calls atomic.Int32
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if info.FullMethod == pb.NotesService_AppendToNoteByPath_FullMethodName && err == nil && calls.Add(1) == 1 {
			return nil, status.Error(codes.Unavailable, "synthetic lost response after commit")
		}
		return response, err
	}))
	pb.RegisterNotesServiceServer(server, NewDefaultNotesServer())
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///synthetic-core", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := pb.NewNotesServiceClient(conn)
	id := "voice:lost-path-response"
	request := &pb.AppendByPathRequest{Relpath: relpath, Text: "exactly one review", OperationId: &id}
	_, err = client.AppendToNoteByPath(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	response, err := client.AppendToNoteByPath(t.Context(), request)
	require.NoError(t, err)
	require.True(t, response.Success)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(content), request.Text))
	require.Equal(t, 1, strings.Count(string(content), appendMarkerPrefix))
	request.Text = "different review"
	_, err = client.AppendToNoteByPath(t.Context(), request)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	invalid := "bad id"
	request.OperationId = &invalid
	_, err = client.AppendToNoteByPath(t.Context(), request)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	request.OperationId, request.Text = nil, "ordinary append"
	_, err = client.AppendToNoteByPath(t.Context(), request)
	require.NoError(t, err)
	_, err = client.AppendToNoteByPath(t.Context(), request)
	require.NoError(t, err)
	content, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(string(content), request.Text), "unkeyed callers retain their previous behavior")
}
