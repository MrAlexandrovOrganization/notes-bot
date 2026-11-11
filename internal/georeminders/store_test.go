package georeminders

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_GEO_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_GEO_DATABASE_URL to a disposable loopback PostgreSQL")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	local := func(host string) bool {
		ip := net.ParseIP(host)
		return host == "localhost" || ip != nil && ip.IsLoopback()
	}
	require.True(t, local(cfg.ConnConfig.Host))
	for _, f := range cfg.ConnConfig.Fallbacks {
		require.True(t, local(f.Host))
	}
	require.Contains(t, []string{"georeminders_test", "voicejobs_test"}, cfg.ConnConfig.Database)
	admin, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	schema := "georeminders_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		require.NoError(t, err)
		admin.Close()
	})
	cfg = cfg.Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = quoted
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	s := &Store{Pool: pool}
	require.Error(t, s.Check(t.Context()))
	require.NoError(t, s.Migrate(t.Context()))
	require.NoError(t, s.Migrate(t.Context()))
	require.NoError(t, s.Check(t.Context()))
	return s
}
func TestDurableRulesAndDeliveries(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	place := &loc.Place{Id: uuid.NewString(), Name: "Synthetic home", Version: 1}
	places := map[string]*loc.Place{place.Id: place}
	now := time.Now().UTC().Truncate(time.Microsecond)
	add := func(repeat string) *pb.Rule {
		r := &pb.Rule{PlaceId: place.Id, Title: "synthetic " + repeat, Transition: "exited", Repeat: repeat}
		require.NoError(t, validateRule(r))
		require.NoError(t, s.Create(ctx, 101, r, uuid.NewString()))
		return r
	}
	once, daily, always := add("once"), add("daily"), add("always")
	_, err := s.Pool.Exec(ctx, "UPDATE geo_rules SET created_at=$1", now.Add(-time.Hour))
	require.NoError(t, err)
	event := func(seq int64, at time.Time) *loc.PlaceEvent {
		return &loc.PlaceEvent{Sequence: seq, PlaceId: place.Id, PlaceVersion: 1, Transition: "exited", OccurredAt: timestamppb.New(at.Add(-time.Minute)), ConfirmedAt: timestamppb.New(at)}
	}
	e := event(1, now.Add(-30*time.Second))
	feed := &loc.ListEventsResponse{Events: []*loc.PlaceEvent{e}, NextSequence: 1}
	// Two consumers racing the same cursor still create one execution per rule.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { errs <- s.Apply(ctx, 0, feed, places, now) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	count := func() int {
		var n int
		require.NoError(t, s.Pool.QueryRow(ctx, "SELECT count(*) FROM geo_deliveries").Scan(&n))
		return n
	}
	require.Equal(t, 3, count())
	require.NoError(t, s.Apply(ctx, 1, &loc.ListEventsResponse{Events: []*loc.PlaceEvent{event(2, now)}, NextSequence: 2}, places, now))
	require.Equal(t, 4, count())
	// An old event and an event predating rule creation cannot arm a new rule.
	add("once")
	require.NoError(t, s.Apply(ctx, 2, &loc.ListEventsResponse{Events: []*loc.PlaceEvent{event(3, now.Add(-time.Hour))}, NextSequence: 3}, places, now))
	require.Equal(t, 4, count())
	rules, err := s.Rules(ctx, 101)
	require.NoError(t, err)
	require.False(t, rules[0].Armed)
	require.Equal(t, once.Id, rules[0].Id)
	require.Equal(t, daily.Id, rules[1].Id)
	require.Equal(t, always.Id, rules[2].Id)
	none, err := s.Claim(ctx, 202)
	require.NoError(t, err)
	require.Nil(t, none)
	first, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.NotNil(t, first)
	_, err = s.Pool.Exec(ctx, "UPDATE geo_deliveries SET lease_until=now()-interval '1 second' WHERE id=$1", first.Id)
	require.NoError(t, err)
	second, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, first.Id, second.Id)
	require.NotEqual(t, first.LeaseToken, second.LeaseToken)
	require.ErrorIs(t, s.Complete(ctx, 101, first.Id, first.LeaseToken, true), pgx.ErrNoRows)
	require.NoError(t, s.Complete(ctx, 101, second.Id, second.LeaseToken, false))
	_, err = s.Pool.Exec(ctx, "UPDATE geo_deliveries SET next_attempt_at=now()-interval '1 second' WHERE id=$1", second.Id)
	require.NoError(t, err)
	third, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, second.Id, third.Id)
	require.NoError(t, s.Complete(ctx, 101, third.Id, third.LeaseToken, true))
	// Expiration and cancellation are persisted rather than silently dropped.
	_, err = s.Pool.Exec(ctx, "UPDATE geo_deliveries SET expires_at=now()-interval '1 second' WHERE status='pending'")
	require.NoError(t, err)
	none, err = s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none)
	var expired int
	require.NoError(t, s.Pool.QueryRow(ctx, "SELECT count(*) FROM geo_deliveries WHERE status='expired'").Scan(&expired))
	require.Equal(t, 3, expired)
	cursor, err := s.Cursor(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, cursor)
}
func TestDisabledPlaceAndFutureRules(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	p := &loc.Place{Id: uuid.NewString(), Version: 2}
	r := &pb.Rule{PlaceId: p.Id, Title: "test", Transition: "entered", Repeat: "always"}
	require.NoError(t, validateRule(r))
	require.NoError(t, s.Create(ctx, 101, r, uuid.NewString()))
	now := time.Now().UTC()
	at := now.Add(-time.Minute)
	feed := &loc.ListEventsResponse{NextSequence: 1, Events: []*loc.PlaceEvent{{Sequence: 1, PlaceId: p.Id, PlaceVersion: 1, Transition: "entered", OccurredAt: timestamppb.New(at), ConfirmedAt: timestamppb.New(now)}}}
	require.NoError(t, s.Apply(ctx, 0, feed, map[string]*loc.Place{p.Id: p}, now))
	d, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, d)
	feed.NextSequence = 2
	feed.Events[0].Sequence = 2
	feed.Events[0].PlaceVersion = 2
	require.NoError(t, s.Apply(ctx, 1, feed, map[string]*loc.Place{p.Id: p}, now))
	d, err = s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, d)
}
func TestValidationAndOwnership(t *testing.T) {
	server := &Server{Owner: 101}
	_, err := server.ListPlaces(t.Context(), &pb.ListPlacesRequest{UserId: 202})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	for _, r := range []*pb.Rule{nil, {PlaceId: uuid.NewString(), Title: "", Transition: "entered", Repeat: "once"}, {PlaceId: uuid.NewString(), Title: "test", Transition: "invalid", Repeat: "once"}, {PlaceId: uuid.NewString(), Title: "test", Transition: "entered", Repeat: "once", Timezone: "NoSuch/Zone"}} {
		require.Error(t, validateRule(r))
	}
}
