package mobilevoice

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/whisper"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_VOICE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_VOICE_DATABASE_URL to an isolated local PostgreSQL database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	local := func(host string) bool { return host == "localhost" || net.ParseIP(host).IsLoopback() }
	require.True(t, local(cfg.ConnConfig.Host))
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		require.True(t, local(fallback.Host))
	}
	admin, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(admin.Close)

	schema := pgx.Identifier{"mobile_voice_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, err)
	})
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return &Store{Pool: pool}
}

type testTranscriber struct {
	t          *testing.T
	upload     Upload
	format     string
	requestID  string
	submits    int
	checkpoint func()
}

func (s *testTranscriber) SubmitOnce(_ context.Context, audio io.Reader, format, preset, identity string) (string, int, error) {
	b, err := io.ReadAll(audio)
	require.NoError(s.t, err)
	require.Equal(s.t, s.upload.Audio, b)
	require.Equal(s.t, s.format, format)
	require.Equal(s.t, "voice", preset)
	require.Equal(s.t, s.requestID, identity)
	s.submits++
	if s.submits == 1 {
		// The backend accepted the upload but the caller missed its reply.
		return "", 0, status.Error(codes.Unavailable, "synthetic interruption")
	}
	return "test-remote", 1, nil
}

func (s *testTranscriber) GetStatus(_ context.Context, jobID string) (*clients.JobResult, error) {
	require.Equal(s.t, "test-remote", jobID)
	return &clients.JobResult{Status: pb.JobStatus_DONE, Text: "Synthetic transcript"}, nil
}

func (s *testTranscriber) Ack(_ context.Context, jobID string) (bool, error) {
	require.Equal(s.t, "test-remote", jobID)
	s.checkpoint()
	return false, status.Error(codes.NotFound, "ack was already recorded")
}

type testNotes struct {
	t       *testing.T
	upload  Upload
	opID    string
	written map[string]string
	calls   int
}

func (n *testNotes) EnsureNote(context.Context, string) (bool, error) {
	n.t.Fatal("path recording must not ensure a daily note")
	return false, nil
}

func (n *testNotes) AppendToNoteOnce(context.Context, string, string, string) (bool, error) {
	n.t.Fatal("path recording must not append to a daily note")
	return false, nil
}

func (n *testNotes) AppendToNoteByPathOnce(_ context.Context, path, text, identity string) (bool, error) {
	require.Equal(n.t, n.upload.Path, path)
	require.Equal(n.t, n.opID, identity)
	require.Equal(n.t, "Synthetic transcript", text)
	n.written[identity] = text
	n.calls++
	if n.calls == 1 {
		return false, status.Error(codes.Unavailable, "synthetic interruption after note write")
	}
	return true, nil
}

func TestDurableVoiceCheckpointsAndReplays(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	require.Error(t, store.CheckSchema(ctx))
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.CheckSchema(ctx))

	upload := Upload{ID: uuid.NewString(), Path: "Папка/Запись.md", Audio: []byte("synthetic audio")}
	s, created, err := store.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, Status{ID: upload.ID, State: Queued}, s)
	s, created, err = store.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, Queued, s.State)
	wrong := upload
	wrong.Path = "Другая.md"
	_, _, err = store.Enqueue(ctx, wrong)
	require.ErrorIs(t, err, ErrConflict)
	wrong = upload
	wrong.Audio = []byte("different")
	_, _, err = store.Enqueue(ctx, wrong)
	require.ErrorIs(t, err, ErrConflict)

	stale, err := store.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, stale)
	blocked, err := store.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, blocked)
	_, err = store.Pool.Exec(ctx, "UPDATE mobile_voice_jobs SET lease_until = now() WHERE id = $1", upload.ID)
	require.NoError(t, err)
	job, err := store.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.ErrorIs(t, store.Release(ctx, stale, 0), errLeaseLost)
	require.NoError(t, store.Release(ctx, job, 0))

	whisper := &testTranscriber{t: t, upload: upload, format: "mp4", requestID: "notes-mobile:" + upload.ID, checkpoint: func() {
		var persisted string
		var hasAudio bool
		err := store.Pool.QueryRow(ctx, "SELECT transcript, audio IS NOT NULL FROM mobile_voice_jobs WHERE id = $1", upload.ID).Scan(&persisted, &hasAudio)
		require.NoError(t, err)
		require.Equal(t, "Synthetic transcript", persisted)
		require.False(t, hasAudio)
	}}
	notes := &testNotes{t: t, upload: upload, opID: "mobile-voice:" + upload.ID, written: make(map[string]string)}
	step := func(want string) {
		// New instance on every step simulates restart and loss of memory state.
		worker := NewWorker(&Store{Pool: store.Pool}, whisper, notes, zap.NewNop())
		require.Zero(t, worker.step(ctx))
		s, err := store.Get(ctx, upload.ID)
		require.NoError(t, err)
		require.Equal(t, want, s.State)
		_, err = store.Pool.Exec(ctx, "UPDATE mobile_voice_jobs SET next_at = now() WHERE id = $1", upload.ID)
		require.NoError(t, err)
	}
	step(Queued)
	step(Transcribing)
	step(Transcribing)
	step(Transcribing)
	step(Saved)
	require.Equal(t, 2, whisper.submits)
	require.Equal(t, 2, notes.calls)
	require.Len(t, notes.written, 1)
	s, created, err = store.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, Saved, s.State)
}

func TestWebVoiceMigrationLeavesLegacyMobileQueueRunning(t *testing.T) {
	mobile := testStore(t)
	ctx := t.Context()
	_, err := mobile.Pool.Exec(ctx, `CREATE TABLE mobile_voice_schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
	require.NoError(t, err)
	_, err = mobile.Pool.Exec(ctx, sql("migrations/001"))
	require.NoError(t, err)
	_, err = mobile.Pool.Exec(ctx, sql("sql/migration_record"))
	require.NoError(t, err)
	web := NewWebStore(mobile.Pool)
	require.Error(t, web.CheckSchema(ctx))
	legacy := Upload{ID: uuid.NewString(), Path: "Synthetic/mobile.md", Audio: []byte("synthetic MP4")}
	_, created, err := mobile.Enqueue(ctx, legacy)
	require.NoError(t, err)
	require.True(t, created)
	job, err := mobile.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, job.ID)
	job.State, job.RemoteID = Transcribing, "synthetic-legacy-id"
	require.NoError(t, mobile.Release(ctx, job, 0))

	require.NoError(t, mobile.Migrate(ctx))
	require.NoError(t, mobile.Migrate(ctx))
	var legacyVersion int
	require.NoError(t, mobile.Pool.QueryRow(ctx, "SELECT max(version) FROM mobile_voice_schema_migrations").Scan(&legacyVersion))
	require.Equal(t, 1, legacyVersion, "older mobile binaries check for exactly version 1")
	require.NoError(t, web.CheckSchema(ctx))
	job, err = web.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, job, "web worker must not process existing mobile jobs")

	// Use the same operation UUID to exercise per-source deduplication and
	// simulate a browser upload whose first response was lost.
	upload := Upload{ID: legacy.ID, Path: "Synthetic/web.md", Audio: []byte("\x1a\x45\xdf\xa3\x9f\x42\x82\x84webm")}
	state, created, err := web.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, Queued, state.State)
	state, created, err = web.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, Queued, state.State)
	conflict := upload
	conflict.Audio = append([]byte("\x1a\x45\xdf\xa3"), 'a')
	_, _, err = web.Enqueue(ctx, conflict)
	require.ErrorIs(t, err, ErrConflict)

	job, err = mobile.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, legacy.Path, job.UploadPath)
	require.Equal(t, "synthetic-legacy-id", job.RemoteID)
	job.State = Saved
	require.NoError(t, mobile.Release(ctx, job, 0))
	job, err = mobile.Claim(ctx)
	require.NoError(t, err)
	require.Nil(t, job, "legacy mobile claim cannot see the browser's queued recording")

	whisper := &testTranscriber{t: t, upload: upload, format: "webm", requestID: "notes-web:" + upload.ID, checkpoint: func() {}}
	notes := &testNotes{t: t, upload: upload, opID: "web-voice:" + upload.ID, written: make(map[string]string)}
	for _, expected := range []string{Queued, Transcribing, Transcribing, Transcribing, Saved} {
		worker := NewWorker(NewWebStore(mobile.Pool), whisper, notes, zap.NewNop())
		require.Zero(t, worker.step(ctx))
		state, err := web.Get(ctx, upload.ID)
		require.NoError(t, err)
		require.Equal(t, expected, state.State)
		_, err = mobile.Pool.Exec(ctx, "UPDATE web_voice_jobs SET next_at = now() WHERE id = $1", upload.ID)
		require.NoError(t, err)
	}
	require.Equal(t, 2, whisper.submits)
	require.Equal(t, 2, notes.calls)
	require.Len(t, notes.written, 1)
	state, created, err = web.Enqueue(ctx, upload)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, Saved, state.State)
}

type orderedTranscriber struct {
	results map[string]*clients.JobResult
}

func (*orderedTranscriber) SubmitOnce(_ context.Context, _ io.Reader, _, _, identity string) (string, int, error) {
	return identity, 1, nil
}

func (s *orderedTranscriber) GetStatus(_ context.Context, id string) (*clients.JobResult, error) {
	return s.results[id], nil
}

func (*orderedTranscriber) Ack(context.Context, string) (bool, error) { return true, nil }

type orderedNotes struct {
	written []string
}

func (*orderedNotes) EnsureNote(context.Context, string) (bool, error) { return true, nil }

func (n *orderedNotes) AppendToNoteOnce(_ context.Context, _, text, _ string) (bool, error) {
	n.written = append(n.written, text)
	return true, nil
}

func (n *orderedNotes) AppendToNoteByPathOnce(ctx context.Context, path, text, id string) (bool, error) {
	return n.AppendToNoteOnce(ctx, path, text, id)
}

func TestVoiceDeliveryKeepsArrivalOrderAcrossTranscriptionSpeedsAndQueues(t *testing.T) {
	mobile := testStore(t)
	ctx := t.Context()
	require.NoError(t, mobile.Migrate(ctx))
	web := NewWebStore(mobile.Pool)

	long := Upload{ID: uuid.NewString(), Date: "27-Nov-2026", Audio: []byte("synthetic MP4")}
	short := Upload{ID: uuid.NewString(), Path: "Daily/27-Nov-2026.md", Audio: []byte("\x1a\x45\xdf\xa3\x9f\x42\x82\x84webm")}
	other := Upload{ID: uuid.NewString(), Path: "Synthetic/unrelated.md", Audio: short.Audio}
	_, _, err := mobile.Enqueue(ctx, long)
	require.NoError(t, err)
	_, _, err = web.Enqueue(ctx, short)
	require.NoError(t, err)
	_, _, err = web.Enqueue(ctx, other)
	require.NoError(t, err)
	// A replay after a lost receipt must preserve the original order.
	_, created, err := mobile.Enqueue(ctx, long)
	require.NoError(t, err)
	require.False(t, created)

	results := &orderedTranscriber{results: map[string]*clients.JobResult{
		"notes-mobile:" + long.ID: {Status: pb.JobStatus_RUNNING},
		"notes-web:" + short.ID:   {Status: pb.JobStatus_DONE, Text: "short"},
		"notes-web:" + other.ID:   {Status: pb.JobStatus_DONE, Text: "unrelated"},
	}}
	notes := &orderedNotes{}
	step := func(store *Store, upload Upload, state string, ready bool) {
		t.Helper()
		// Control which job is due to simulate arbitrary worker timing and restarts.
		table := "mobile_voice_jobs"
		if store.web {
			table = "web_voice_jobs"
		}
		_, err := store.Pool.Exec(ctx, "UPDATE "+table+
			" SET next_at = CASE WHEN id = $1 THEN now() ELSE now() + interval '1 hour' END", upload.ID)
		require.NoError(t, err)
		job, err := store.Claim(ctx)
		require.NoError(t, err)
		require.NotNil(t, job)
		require.Equal(t, upload.ID, job.ID)
		delay := NewWorker(store, results, notes, zap.NewNop()).advance(ctx, job)
		require.NoError(t, store.Release(ctx, job, delay))
		require.Equal(t, state, job.State)
		require.Equal(t, ready, job.ResultReady)
	}
	step(mobile, long, Transcribing, false)
	step(mobile, long, Transcribing, false)
	step(web, short, Transcribing, false)
	step(web, short, Transcribing, true)
	step(web, short, Transcribing, true)
	require.Empty(t, notes.written, "fast transcription must wait for the first recording")
	step(web, other, Transcribing, false)
	step(web, other, Transcribing, true)
	step(web, other, Saved, true)
	require.Equal(t, []string{"unrelated"}, notes.written)

	results.results["notes-mobile:"+long.ID] = &clients.JobResult{Status: pb.JobStatus_DONE, Text: "long"}
	step(mobile, long, Transcribing, true)
	step(mobile, long, Saved, true)
	step(web, short, Saved, true)
	require.Equal(t, []string{"unrelated", "long", "short"}, notes.written)
}
