package automations

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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/internal/automationevents"
	pb "notes-bot/proto/automation/v1"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_AUTOMATIONS_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_AUTOMATIONS_DATABASE_URL to a disposable loopback PostgreSQL")
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
	require.Contains(t, []string{"automations_test", "voicejobs_test"}, cfg.ConnConfig.Database)
	admin, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	schema := "automations_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func taskEvent(id string, revision uint64, status pb.TaskStatus, at time.Time) *pb.Event {
	before := pb.TaskStatus_TASK_STATUS_PENDING
	if status == before {
		before = pb.TaskStatus_TASK_STATUS_COMPLETED
	}
	return &pb.Event{Id: uuid.NewString(), Source: automationevents.CoreSource, Type: automationevents.TaskStatusChangedType, Subject: automationevents.Subject("12-Nov-2026", id), OccurredAt: timestamppb.New(at), Data: &pb.Event_TaskStatusChanged{TaskStatusChanged: &pb.TaskStatusChanged{Date: "12-Nov-2026", TaskId: id, Title: "Synthetic task", Before: before, After: status, Revision: revision}}}
}
func taskRule(id string, delay int32, repeat bool) *pb.Rule {
	return &pb.Rule{Trigger: &pb.Rule_TaskStatus{TaskStatus: &pb.TaskTrigger{Date: "12-Nov-2026", TaskId: id, TargetStatus: pb.TaskStatus_TASK_STATUS_COMPLETED}}, Action: &pb.Rule_Notify{Notify: &pb.NotifyAction{Text: "Synthetic notification"}}, DelaySeconds: delay, Repeat: repeat}
}
func TestDurableAutomationExecutions(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	id := uuid.NewString()
	once := taskRule(id, 0, false)
	delayed := taskRule(id, 3600, true)
	require.NoError(t, validateRule(once))
	require.NoError(t, validateRule(delayed))
	request := uuid.NewString()
	require.NoError(t, s.Create(ctx, 101, request, once))
	onceID := once.Id
	once.TaskTitle = "Renamed after first request"
	require.NoError(t, s.Create(ctx, 101, request, once))
	require.Equal(t, onceID, once.Id)
	require.NoError(t, s.Create(ctx, 101, uuid.NewString(), delayed))
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := taskEvent(id, 1, pb.TaskStatus_TASK_STATUS_COMPLETED, now)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { errs <- s.Ingest(ctx, event, now) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	executions, err := s.Executions(ctx, 101)
	require.NoError(t, err)
	require.Len(t, executions, 2)
	corrupted := proto.Clone(event).(*pb.Event)
	corrupted.GetTaskStatusChanged().Title = "changed immutable event"
	require.Error(t, s.Ingest(ctx, corrupted, now))
	none, err := s.Claim(ctx, 202)
	require.NoError(t, err)
	require.Nil(t, none)
	first, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, "Synthetic notification", first.Text)
	none, err = s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none) // delay is independent of delivery grace period
	_, err = s.Pool.Exec(ctx, "UPDATE automation_executions SET lease_until=now()-interval '1 second' WHERE id=$1", first.Id)
	require.NoError(t, err)
	reclaimed, err := s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, first.Id, reclaimed.Id)
	require.NotEqual(t, first.LeaseToken, reclaimed.LeaseToken)
	require.ErrorIs(t, s.Complete(ctx, 101, first.Id, first.LeaseToken, true), pgx.ErrNoRows)
	require.NoError(t, s.Complete(ctx, 101, reclaimed.Id, reclaimed.LeaseToken, true))
	// A later completion is a new event. Once is consumed; repeating rule remains armed.
	require.NoError(t, s.Ingest(ctx, taskEvent(id, 2, pb.TaskStatus_TASK_STATUS_PENDING, now.Add(time.Second)), now))
	require.NoError(t, s.Ingest(ctx, taskEvent(id, 3, pb.TaskStatus_TASK_STATUS_COMPLETED, now.Add(2*time.Second)), now.Add(2*time.Second)))
	executions, err = s.Executions(ctx, 101)
	require.NoError(t, err)
	require.Len(t, executions, 3)
	_, err = s.Pool.Exec(ctx, query("delete"), 101, delayed.Id)
	require.NoError(t, err)
	none, err = s.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none)
	executions, err = s.Executions(ctx, 101)
	require.NoError(t, err)
	cancelled := 0
	for _, e := range executions {
		if e.Status == "cancelled" {
			cancelled++
		}
	}
	require.Equal(t, 2, cancelled)
}
func TestOldEventsDoNotConsumeNewRules(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	id := uuid.NewString()
	rule := taskRule(id, 0, false)
	require.NoError(t, s.Create(ctx, 101, uuid.NewString(), rule))
	now := time.Now().UTC()
	require.NoError(t, s.Ingest(ctx, taskEvent(id, 1, pb.TaskStatus_TASK_STATUS_COMPLETED, now.Add(-time.Hour)), now))
	jobs, err := s.Executions(ctx, 101)
	require.NoError(t, err)
	require.Empty(t, jobs)
	rules, err := s.Rules(ctx, 101)
	require.NoError(t, err)
	require.True(t, rules[0].Armed)
	_, err = s.Pool.Exec(ctx, "UPDATE automation_rules SET created_at=$1", now.Add(-72*time.Hour))
	require.NoError(t, err)
	require.NoError(t, s.Ingest(ctx, taskEvent(id, 2, pb.TaskStatus_TASK_STATUS_COMPLETED, now.Add(-48*time.Hour)), now))
	jobs, err = s.Executions(ctx, 101)
	require.NoError(t, err)
	require.Empty(t, jobs)
	rules, err = s.Rules(ctx, 101)
	require.NoError(t, err)
	require.True(t, rules[0].Armed)
}

func TestDelayedActionSurvivesRestartAndExpires(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	id := uuid.NewString()
	rule := taskRule(id, 1800, false)
	require.NoError(t, s.Create(ctx, 101, uuid.NewString(), rule))
	now := time.Now().UTC()
	require.NoError(t, s.Ingest(ctx, taskEvent(id, 1, pb.TaskStatus_TASK_STATUS_COMPLETED, now), now))
	restarted := &Store{Pool: s.Pool}
	none, err := restarted.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = s.Pool.Exec(ctx, "UPDATE automation_executions SET due_at=now()-interval '1 second',next_attempt_at=now()-interval '1 second'")
	require.NoError(t, err)
	due, err := restarted.Claim(ctx, 101)
	require.NoError(t, err)
	require.NotNil(t, due)
	require.NoError(t, restarted.Complete(ctx, 101, due.Id, due.LeaseToken, false))
	none, err = restarted.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = s.Pool.Exec(ctx, "UPDATE automation_executions SET expires_at=now()-interval '1 second'")
	require.NoError(t, err)
	none, err = restarted.Claim(ctx, 101)
	require.NoError(t, err)
	require.Nil(t, none)
	history, err := restarted.Executions(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, "expired", history[0].Status)
}
