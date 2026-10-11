package core

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"notes-bot/core/features"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "notes-bot/proto/notes"
)

func TestAppendOnceRetryConflictAndMarker(t *testing.T) {
	daily := setupNotesEnv(t)
	store := &realNoteStore{}
	date, text, operationID := "12-Oct-2026", "Синтетическая заметка 🌍\nвторая строка", "voice:test-operation"
	require.NoError(t, store.AppendToNoteOnce(t.Context(), date, text, operationID))
	path := filepath.Join(daily, date+".md")
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, store.AppendToNoteOnce(t.Context(), date, text, operationID))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, strings.Count(string(second), text))
	assert.Equal(t, 1, strings.Count(string(second), appendMarkerPrefix))
	assert.ErrorIs(t, store.AppendToNoteOnce(t.Context(), date, "different text", operationID), ErrOperationConflict)
	assert.ErrorIs(t, store.AppendToNoteOnce(t.Context(), "13-Oct-2026", text, operationID), ErrOperationConflict)
	_, err = os.Stat(filepath.Join(daily, "13-Oct-2026.md"))
	assert.True(t, os.IsNotExist(err))
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, first, unchanged)
}

func TestAppendOnceValidationAndOrdinaryAppend(t *testing.T) {
	daily := setupNotesEnv(t)
	store := &realNoteStore{}
	for _, id := range []string{"", "bad operation", "../path", "я", strings.Repeat("a", 129)} {
		assert.ErrorIs(t, store.AppendToNoteOnce(t.Context(), "12-Oct-2026", "text", id), ErrInvalidOperationID)
	}
	assert.Error(t, store.AppendToNoteOnce(t.Context(), "../../outside", "text", "valid"))
	assert.Error(t, store.AppendToNoteOnce(t.Context(), "12-Oct-2026", appendMarkerPrefix+"forged -->", "valid"))
	require.NoError(t, store.AppendToNote(t.Context(), "12-Oct-2026", "ordinary"))
	require.NoError(t, store.AppendToNote(t.Context(), "12-Oct-2026", "ordinary"))
	content, err := os.ReadFile(filepath.Join(daily, "12-Oct-2026.md"))
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(content), "ordinary"))
	assert.NotContains(t, string(content), appendMarkerPrefix)
}

func TestAppendOnceMarkerSurvivesThreeStateTaskWriter(t *testing.T) {
	daily := setupNotesEnv(t)
	date, text, operationID := "12-Oct-2026", "synthetic durable voice delivery", "voice:task-writer"
	notes, tasks := &realNoteStore{}, &realTaskStore{}
	require.NoError(t, notes.EnsureNote(t.Context(), date))
	require.NoError(t, tasks.AddTask(t.Context(), date, "synthetic task"))
	require.NoError(t, notes.AppendToNoteOnce(t.Context(), date, text, operationID))
	path := filepath.Join(daily, date+".md")
	for _, state := range []features.TaskState{features.TaskStateCompleted, features.TaskStateRejected, features.TaskStatePending} {
		require.NoError(t, tasks.SetTaskStatus(t.Context(), date, 0, state))
		beforeRetry, err := os.ReadFile(path)
		require.NoError(t, err)
		parsed := features.ParseTasks(t.Context(), string(beforeRetry))
		require.Len(t, parsed, 1)
		assert.Equal(t, state, parsed[0].State)
		require.NoError(t, notes.AppendToNoteOnce(t.Context(), date, text, operationID))
		afterRetry, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, beforeRetry, afterRetry)
		assert.Equal(t, 1, strings.Count(string(afterRetry), text))
		assert.Equal(t, 1, strings.Count(string(afterRetry), appendMarkerPrefix))
		assert.ErrorIs(t, notes.AppendToNoteOnce(t.Context(), date, "different delivery", operationID), ErrOperationConflict)
	}

	// The newly merged writer must take the same sidecar lock as append. A
	// cancelled waiter cannot modify the note while another process owns it.
	unlock, err := lockFile(t.Context(), path)
	require.NoError(t, err)
	defer unlock()
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, tasks.SetTaskStatus(ctx, date, 0, features.TaskStateCompleted), context.DeadlineExceeded)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestAppendOnceLostGRPCResponseDoesNotDuplicate(t *testing.T) {
	daily := setupNotesEnv(t)
	listener := bufconn.Listen(1024 * 1024)
	var calls atomic.Int32
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if info.FullMethod == pb.NotesService_AppendToNote_FullMethodName && err == nil && calls.Add(1) == 1 {
			return nil, status.Error(codes.Unavailable, "synthetic lost response after commit")
		}
		return response, err
	}))
	pb.RegisterNotesServiceServer(server, NewDefaultNotesServer())
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///synthetic-core", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	client := pb.NewNotesServiceClient(connection)
	id := "voice:lost-response"
	request := &pb.AppendRequest{Date: "12-Oct-2026", Text: "exactly one synthetic delivery", OperationId: &id}
	_, err = client.AppendToNote(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	response, err := client.AppendToNote(t.Context(), request)
	require.NoError(t, err)
	assert.True(t, response.Success)
	content, err := os.ReadFile(filepath.Join(daily, request.Date+".md"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(content), request.Text))
	assert.Equal(t, 1, strings.Count(string(content), appendMarkerPrefix))
	request.Text = "different payload"
	_, err = client.AppendToNote(t.Context(), request)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
	badID := "bad id"
	request.OperationId = &badID
	_, err = client.AppendToNote(t.Context(), request)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAppendOnceSubprocessHelper(t *testing.T) {
	vault := os.Getenv("NOTES_APPEND_TEST_VAULT")
	if vault == "" {
		t.Skip("only runs as an isolated child of the locking test")
	}
	instance = &Config{NotesDir: vault, DailyNotesDir: filepath.Join(vault, "Daily"), DailyTemplatePath: filepath.Join(vault, "Templates", "Daily.md")}
	once.Do(func() {})
	store := &realNoteStore{}
	if os.Getenv("NOTES_APPEND_TEST_ORDINARY") == "1" {
		require.NoError(t, store.AppendToNote(t.Context(), "12-Oct-2026", "ordinary-process"))
	} else {
		require.NoError(t, store.AppendToNoteOnce(t.Context(), "12-Oct-2026", "idempotent-process", "process:one"))
	}
}

func TestAppendOnceCrossProcessLockPreservesAllWrites(t *testing.T) {
	daily := setupNotesEnv(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	var group sync.WaitGroup
	results := make(chan error, 8)
	for i := range 8 {
		group.Go(func() {
			command := exec.CommandContext(t.Context(), executable, "-test.run=^TestAppendOnceSubprocessHelper$")
			command.Env = append(os.Environ(), "NOTES_APPEND_TEST_VAULT="+filepath.Dir(daily))
			if i%2 == 0 {
				command.Env = append(command.Env, "NOTES_APPEND_TEST_ORDINARY=1")
			}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Logf("child failure: %s", output)
			}
			results <- err
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	content, err := os.ReadFile(filepath.Join(daily, "12-Oct-2026.md"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(content), "idempotent-process"))
	assert.Equal(t, 4, strings.Count(string(content), "ordinary-process"))
	assert.Equal(t, 1, strings.Count(string(content), appendMarkerPrefix))
}

func TestNoteLockCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	unlock, err := lockFile(t.Context(), path)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = lockFile(ctx, path)
	assert.ErrorIs(t, err, context.Canceled)
}
