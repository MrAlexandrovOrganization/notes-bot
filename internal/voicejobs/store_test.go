package voicejobs

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Each test owns a random schema in an explicitly configured local test DB.
// Neither the application's environment nor a default production DSN is used.
func testStore(t *testing.T, migrate bool) (*Store, *pgxpool.Config) {
	t.Helper()
	dsn := os.Getenv("TEST_VOICE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_VOICE_DATABASE_URL to an isolated local PostgreSQL database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid TEST_VOICE_DATABASE_URL")
	}
	local := func(host string) bool {
		ip := net.ParseIP(host)
		return host == "localhost" || ip != nil && ip.IsLoopback()
	}
	if !local(cfg.ConnConfig.Host) {
		t.Fatal("TEST_VOICE_DATABASE_URL must use a loopback host")
	}
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		if !local(fallback.Host) {
			t.Fatal("TEST_VOICE_DATABASE_URL must use loopback fallback hosts")
		}
	}
	cfg.MaxConns = 16
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	ctx := t.Context()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	if err = admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatal("local voice test database unavailable")
	}
	schema := "voicejobs_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, cleanupErr := admin.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		if cleanupErr != nil {
			t.Error("failed to remove isolated voice test schema")
		}
		admin.Close()
	})
	cfg = cfg.Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := &Store{Pool: pool}
	if migrate {
		require.NoError(t, store.Migrate(ctx))
	}
	return store, cfg
}

func testJob(chat int64, message int) *Job {
	return &Job{
		ID: uuid.NewString(), ChatID: chat, MessageID: message, UserID: 101,
		FileID: "synthetic-telegram-file", Format: "ogg", ActiveDate: "12-Oct-2026",
		Mode: "note", SentAt: time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC),
		TimezoneOffset: 3, DayStartHour: 4, StatusMessageID: message + 1000,
		Traceparent: "00-11111111111111111111111111111111-2222222222222222-01",
	}
}

func enqueue(t *testing.T, s *Store, j *Job) {
	t.Helper()
	require.NoError(t, s.Enqueue(t.Context(), j))
}

func claim(t *testing.T, s *Store) *Job {
	t.Helper()
	j, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.NotNil(t, j)
	require.NotEmpty(t, j.LeaseToken)
	return j
}

func TestStoreMigrationRepeatPreservesJobsAndOtherTables(t *testing.T) {
	s, _ := testStore(t, false)
	ctx := t.Context()
	require.Error(t, s.CheckSchema(ctx))
	_, err := s.Pool.Exec(ctx, "CREATE TABLE unrelated_fixture (value text NOT NULL); INSERT INTO unrelated_fixture VALUES ('preserved')")
	require.NoError(t, err)
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.CheckSchema(ctx))
	j := testJob(100, 1)
	enqueue(t, s, j)
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Migrate(ctx))
	got, err := s.Find(ctx, j.ChatID, j.MessageID)
	require.NoError(t, err)
	require.Equal(t, j, got)
	var value string
	require.NoError(t, s.Pool.QueryRow(ctx, "SELECT value FROM unrelated_fixture").Scan(&value))
	require.Equal(t, "preserved", value)
	var versions int
	require.NoError(t, s.Pool.QueryRow(ctx, "SELECT count(*) FROM voice_schema_migrations").Scan(&versions))
	require.Equal(t, 1, versions)
}

func TestStoreDuplicatePreservesOriginalSnapshot(t *testing.T) {
	s, _ := testStore(t, true)
	original := testJob(100, 10)
	enqueue(t, s, original)
	duplicate := testJob(100, 10)
	duplicate.FileID = "different-file"
	duplicate.UserID = 202
	duplicate.ActiveDate = "13-Oct-2026"
	duplicate.Mode = "smart"
	duplicate.SentAt = original.SentAt.Add(24 * time.Hour)
	duplicate.StatusMessageID = 9000
	enqueue(t, s, duplicate)
	got, err := s.Find(t.Context(), original.ChatID, original.MessageID)
	require.NoError(t, err)
	require.Equal(t, original, got)
	otherChat := testJob(200, 10)
	enqueue(t, s, otherChat)
	var count int
	require.NoError(t, s.Pool.QueryRow(t.Context(), "SELECT count(*) FROM voice_jobs").Scan(&count))
	require.Equal(t, 2, count)
}

func TestStoreRestartRecoversProgressAndPrivateLookups(t *testing.T) {
	s, cfg := testStore(t, true)
	j := testJob(100, 10)
	enqueue(t, s, j)
	j = claim(t, s)
	j.RemoteID = "synthetic-backend-job"
	j.Text = "Синтетическая расшифровка & <текст>"
	j.ResultReady, j.Acked, j.NoteSaved = true, true, true
	j.Prepared = []byte(`{"intent":"note","text":"prepared"}`)
	j.LastStatus = "ready"
	require.NoError(t, s.Save(t.Context(), j))
	require.NoError(t, s.Release(t.Context(), j, false, 0))
	s.Pool.Close()
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	restarted := &Store{Pool: pool}
	require.NoError(t, restarted.CheckSchema(t.Context()))
	got := claim(t, restarted)
	require.NotEqual(t, j.LeaseToken, got.LeaseToken)
	expected := *j
	expected.LeaseToken = got.LeaseToken
	require.JSONEq(t, string(j.Prepared), string(got.Prepared))
	expected.Prepared = got.Prepared // PostgreSQL JSONB normalizes whitespace/key order.
	require.Equal(t, &expected, got)
	owner, err := restarted.Get(t.Context(), j.ID, j.UserID)
	require.NoError(t, err)
	require.NotNil(t, owner)
	foreign, err := restarted.Get(t.Context(), j.ID, j.UserID+1)
	require.NoError(t, err)
	require.Nil(t, foreign)
	page, err := restarted.Page(t.Context(), j.ChatID, j.UserID, j.StatusMessageID)
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Equal(t, j.ID, page.ID)
	foreign, err = restarted.Page(t.Context(), j.ChatID+1, j.UserID, j.StatusMessageID)
	require.NoError(t, err)
	require.Nil(t, foreign)
}

func TestStorePreparedCASRequiresDeliveredOwnerAndPreservesWinnerAfterRestart(t *testing.T) {
	s, cfg := testStore(t, true)
	ctx := t.Context()
	j := testJob(100, 10)
	j.Mode = "smart"
	j.Text = "Синтетическое напоминание завтра"
	j.ResultReady = true
	j.Prepared = []byte(`{"intent":{"intent":"unknown","title":"original"}}`)
	enqueue(t, s, j)
	derived := []byte(`{"intent":{"intent":"unknown","title":"original"},"reminder":{"title":"derived","date":"13-Oct-2026"}}`)
	updated, err := s.SetPrepared(ctx, j.ID, j.UserID, j.Prepared, derived)
	require.NoError(t, err)
	require.False(t, updated, "active worker state must not be overwritten without a lease")
	j = claim(t, s)
	require.NoError(t, s.Release(ctx, j, true, 0))
	updated, err = s.SetPrepared(ctx, j.ID, j.UserID+1, j.Prepared, derived)
	require.NoError(t, err)
	require.False(t, updated, "a different user must not refine the prepared snapshot")
	// CAS compares JSON values, not their formatting or object key order.
	equivalent := []byte(`{ "intent": { "title": "original", "intent": "unknown" } }`)
	updated, err = s.SetPrepared(ctx, j.ID, j.UserID, equivalent, derived)
	require.NoError(t, err)
	require.True(t, updated)
	updated, err = s.SetPrepared(ctx, j.ID, j.UserID, equivalent, []byte(`{"reminder":{"title":"losing parse"}}`))
	require.NoError(t, err)
	require.False(t, updated, "a stale callback must not replace the committed winner")
	s.Pool.Close()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	got, err := (&Store{Pool: pool}).Get(ctx, j.ID, j.UserID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.JSONEq(t, string(derived), string(got.Prepared))
	require.Equal(t, j.Text, got.Text)
	require.Equal(t, j.ActiveDate, got.ActiveDate)
	require.Equal(t, j.SentAt, got.SentAt)
}

func TestStoreConcurrentClaimsHaveExclusiveOwners(t *testing.T) {
	s, _ := testStore(t, true)
	const workers = 12
	for i := 0; i < workers; i++ {
		enqueue(t, s, testJob(100, i+1))
	}
	results := make(chan *Job, workers)
	errors := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			<-start
			j, err := s.Claim(t.Context())
			results <- j
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	ids, tokens := map[string]bool{}, map[string]bool{}
	for j := range results {
		require.NotNil(t, j)
		require.False(t, ids[j.ID], "job claimed by more than one owner")
		require.False(t, tokens[j.LeaseToken], "lease tokens must be distinct")
		ids[j.ID], tokens[j.LeaseToken] = true, true
	}
	require.Len(t, ids, workers)
	j, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.Nil(t, j)
}

func TestStoreExpiredLeaseFencesOldOwnerBeforeAndAfterReclaim(t *testing.T) {
	s, _ := testStore(t, true)
	enqueue(t, s, testJob(100, 10))
	old := claim(t, s)
	_, err := s.Pool.Exec(t.Context(), "UPDATE voice_jobs SET lease_until=now()-interval '1 second' WHERE id=$1", old.ID)
	require.NoError(t, err)
	old.Text = "stale result"
	require.ErrorIs(t, s.Save(t.Context(), old), ErrLeaseLost)
	require.ErrorIs(t, s.Release(t.Context(), old, true, 0), ErrLeaseLost)
	current := claim(t, s)
	require.Equal(t, old.ID, current.ID)
	require.NotEqual(t, old.LeaseToken, current.LeaseToken)
	require.Empty(t, current.Text)
	current.Text = "current result"
	current.ResultReady = true
	require.NoError(t, s.Save(t.Context(), current))
	require.ErrorIs(t, s.Save(t.Context(), old), ErrLeaseLost)
	require.ErrorIs(t, s.Release(t.Context(), old, true, 0), ErrLeaseLost)
	require.NoError(t, s.Release(t.Context(), current, true, 0))
	got, err := s.Get(t.Context(), current.ID, current.UserID)
	require.NoError(t, err)
	require.Equal(t, "current result", got.Text)
	next, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.Nil(t, next)
}

func TestStoreCancellationSurvivesStaleSaveAndRelease(t *testing.T) {
	s, _ := testStore(t, true)
	enqueue(t, s, testJob(100, 10))
	j := claim(t, s)
	require.NoError(t, s.Cancel(t.Context(), j.ID, j.UserID+1))
	got, err := s.Get(t.Context(), j.ID, j.UserID)
	require.NoError(t, err)
	require.False(t, got.CancelRequested)
	require.NoError(t, s.Cancel(t.Context(), j.ID, j.UserID))
	require.False(t, j.CancelRequested, "worker still holds the pre-cancellation snapshot")
	j.Text = "completed after cancellation request"
	require.NoError(t, s.Save(t.Context(), j))
	got, err = s.Get(t.Context(), j.ID, j.UserID)
	require.NoError(t, err)
	require.True(t, got.CancelRequested)
	require.NoError(t, s.Release(t.Context(), j, false, 0))
	got = claim(t, s)
	require.True(t, got.CancelRequested)
	require.NoError(t, s.Release(t.Context(), got, true, 0))
	got, err = s.Get(t.Context(), j.ID, j.UserID)
	require.NoError(t, err)
	require.True(t, got.CancelRequested)
}

func TestStoreDeliveryOrderIsPerChatAndUnblocksOnTerminalRelease(t *testing.T) {
	s, _ := testStore(t, true)
	first, third, second := testJob(100, 10), testJob(100, 30), testJob(100, 20)
	for _, j := range []*Job{first, third, second, testJob(200, 1)} {
		enqueue(t, s, j)
	}
	front, err := s.IsFront(t.Context(), first)
	require.NoError(t, err)
	require.True(t, front, "another chat must not block delivery")
	for _, j := range []*Job{second, third} {
		front, err = s.IsFront(t.Context(), j)
		require.NoError(t, err)
		require.False(t, front)
	}
	owners := make(map[string]*Job)
	for range 4 {
		j := claim(t, s)
		owners[j.ID] = j
	}
	// Waiting for a worker/result must retain the earlier delivery slot.
	require.NoError(t, s.Release(t.Context(), owners[first.ID], false, time.Hour))
	front, err = s.IsFront(t.Context(), second)
	require.NoError(t, err)
	require.False(t, front)
	// Make the first job due without waiting or relying on wall-clock sleeps.
	_, err = s.Pool.Exec(t.Context(), "UPDATE voice_jobs SET next_at=now()-interval '1 second' WHERE id=$1", first.ID)
	require.NoError(t, err)
	done := claim(t, s)
	require.Equal(t, first.ID, done.ID)
	done.Error = "terminal synthetic failure"
	require.NoError(t, s.Release(t.Context(), done, true, 0))
	front, err = s.IsFront(t.Context(), second)
	require.NoError(t, err)
	require.True(t, front)
	front, err = s.IsFront(t.Context(), third)
	require.NoError(t, err)
	require.False(t, front)
	require.NoError(t, s.Release(t.Context(), owners[second.ID], true, 0))
	front, err = s.IsFront(t.Context(), third)
	require.NoError(t, err)
	require.True(t, front)
}

func TestStoreCheckSchemaRejectsUnknownVersion(t *testing.T) {
	s, _ := testStore(t, true)
	_, err := s.Pool.Exec(t.Context(), "INSERT INTO voice_schema_migrations(version) VALUES(2)")
	require.NoError(t, err)
	require.Error(t, s.CheckSchema(t.Context()))
}

func TestStoreMetricsObservePendingStateAndDatabaseFailure(t *testing.T) {
	s, _ := testStore(t, true)
	jobs := []*Job{testJob(100, 1), testJob(100, 2), testJob(100, 3), testJob(100, 4)}
	jobs[1].ResultReady = true
	for _, j := range jobs {
		enqueue(t, s, j)
	}
	require.NoError(t, s.Cancel(t.Context(), jobs[2].ID, jobs[2].UserID))
	_, err := s.Pool.Exec(t.Context(), `UPDATE voice_jobs SET
done=(id=$1), created_at=now()-CASE WHEN id=$1 THEN interval '1 hour' ELSE interval '3 minutes' END`, jobs[3].ID)
	require.NoError(t, err)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	registration, err := s.RegisterMetrics()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, registration.Unregister()) })
	collect := func() (map[string]int64, map[string]float64) {
		t.Helper()
		var result metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(t.Context(), &result))
		ints, floats := map[string]int64{}, map[string]float64{}
		for _, scope := range result.ScopeMetrics {
			require.Equal(t, "telegram", scope.Scope.Name)
			for _, m := range scope.Metrics {
				switch data := m.Data.(type) {
				case metricdata.Gauge[int64]:
					require.Len(t, data.DataPoints, 1)
					require.Zero(t, data.DataPoints[0].Attributes.Len())
					ints[m.Name] = data.DataPoints[0].Value
				case metricdata.Gauge[float64]:
					require.Len(t, data.DataPoints, 1)
					require.Zero(t, data.DataPoints[0].Attributes.Len())
					floats[m.Name] = data.DataPoints[0].Value
				}
			}
		}
		return ints, floats
	}
	ints, floats := collect()
	require.EqualValues(t, 3, ints["telegram.voice.jobs.pending"])
	require.EqualValues(t, 1, ints["telegram.voice.jobs.ready_undelivered"])
	require.EqualValues(t, 1, ints["telegram.voice.jobs.cancelled_pending"])
	require.EqualValues(t, 1, ints["telegram.voice.database.available"])
	require.InDelta(t, 180, floats["telegram.voice.jobs.oldest_pending.age"], 10)
	lastSuccess := ints["telegram.voice.database.last_success"]
	require.InDelta(t, time.Now().Unix(), lastSuccess, 10)
	s.Pool.Close()
	ints, _ = collect()
	require.Zero(t, ints["telegram.voice.database.available"])
	require.Equal(t, lastSuccess, ints["telegram.voice.database.last_success"])
	_, hasPending := ints["telegram.voice.jobs.pending"]
	require.False(t, hasPending, "DB failure must not be reported as an empty queue")
}

// Keep this compile-time assertion close to the real database scenarios.
var _ Repository = (*Store)(nil)
