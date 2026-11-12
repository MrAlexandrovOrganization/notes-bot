package automations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/internal/automationevents"
	pb "notes-bot/proto/automation/v1"
)

func ruleBytes(r *pb.Rule) ([]byte, error) {
	r = proto.Clone(r).(*pb.Rule)
	r.Id = 0
	r.Armed = false
	return proto.MarshalOptions{Deterministic: true}.Marshal(r)
}
func (s *Store) Create(ctx context.Context, user int64, request string, r *pb.Rule) error {
	data, err := ruleBytes(r)
	if err != nil {
		return err
	}
	normalized := proto.Clone(r).(*pb.Rule)
	normalized.TaskTitle = ""
	canonical, err := ruleBytes(normalized)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(canonical)
	trigger := r.GetTaskStatus()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, query("migration_lock")); err != nil {
		return err
	}
	var saved []byte
	var id int64
	var armed bool
	if err = tx.QueryRow(ctx, query("create_rule"), user, request, data, hash[:], automationevents.TaskStatusChangedType, automationevents.Subject(trigger.Date, trigger.TaskId), trigger.TargetStatus, r.DelaySeconds, r.Repeat).Scan(&id, &armed, &saved); err != nil {
		return err
	}
	stored := new(pb.Rule)
	if err = proto.Unmarshal(saved, stored); err != nil {
		return err
	}
	proto.Reset(r)
	proto.Merge(r, stored)
	r.Id = id
	r.Armed = armed
	return tx.Commit(ctx)
}
func (s *Store) Rules(ctx context.Context, user int64) ([]*pb.Rule, error) {
	rows, err := s.Pool.Query(ctx, query("rules"), user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pb.Rule
	for rows.Next() {
		var id int64
		var raw []byte
		var armed bool
		if err = rows.Scan(&id, &raw, &armed); err != nil {
			return nil, err
		}
		r := new(pb.Rule)
		if err = proto.Unmarshal(raw, r); err != nil {
			return nil, err
		}
		r.Id = id
		r.Armed = armed
		out = append(out, r)
	}
	return out, rows.Err()
}

// Ingest records the inbox, rule executions and once state in one transaction.
// Acking the source outbox is a separate, repeatable operation after commit.
func (s *Store) Ingest(ctx context.Context, e *pb.Event, now time.Time) error {
	if err := automationevents.Validate(e); err != nil {
		return err
	}
	if e.OccurredAt.AsTime().After(now.Add(30 * time.Second)) {
		return errors.New("event time is in the future")
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(e)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, query("ingest_lock")); err != nil {
		return err
	}
	var previous []byte
	err = tx.QueryRow(ctx, query("inbox_find"), e.Source, e.Id).Scan(&previous)
	if err == nil {
		if !bytes.Equal(previous, hash[:]) {
			return errors.New("event identity conflict")
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	change := e.GetTaskStatusChanged()
	at := e.OccurredAt.AsTime()
	if _, err = tx.Exec(ctx, query("inbox_add"), e.Source, e.Id, e.Subject, change.Revision, hash[:], raw, at); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, query("matching"), e.Type, e.Subject, change.After, at)
	if err != nil {
		return err
	}
	type match struct {
		id, user int64
		payload  []byte
		delay    int32
	}
	var matches []match
	for rows.Next() {
		var m match
		if err = rows.Scan(&m.id, &m.user, &m.payload, &m.delay); err != nil {
			rows.Close()
			return err
		}
		matches = append(matches, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, m := range matches {
		r := new(pb.Rule)
		if err = proto.Unmarshal(m.payload, r); err != nil {
			return err
		}
		due := at.Add(time.Duration(m.delay) * time.Second)
		expiry := due.Add(24 * time.Hour)
		if !expiry.After(now) {
			continue
		}
		if _, err = tx.Exec(ctx, query("enqueue"), m.id, e.Source, e.Id, m.user, r.GetNotify().Text, due, expiry); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, query("fired"), m.id, at); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Claim(ctx context.Context, user int64) (*pb.Delivery, error) {
	if _, err := s.Pool.Exec(ctx, query("expire")); err != nil {
		return nil, err
	}
	d := new(pb.Delivery)
	var expiry time.Time
	err := s.Pool.QueryRow(ctx, query("claim"), user, uuid.NewString()).Scan(&d.Id, &d.LeaseToken, &d.Text, &expiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ExpiresAt = timestamppb.New(expiry)
	return d, nil
}
func (s *Store) Complete(ctx context.Context, user, id int64, token string, success bool) error {
	tag, err := s.Pool.Exec(ctx, query("complete"), id, user, token, success)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
func (s *Store) Executions(ctx context.Context, user int64) ([]*pb.Execution, error) {
	rows, err := s.Pool.Query(ctx, query("executions"), user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pb.Execution
	for rows.Next() {
		e := new(pb.Execution)
		var due time.Time
		var payload []byte
		if err = rows.Scan(&e.Id, &e.RuleId, &e.Status, &e.Attempts, &due, &payload); err != nil {
			return nil, err
		}
		rule := new(pb.Rule)
		if err = proto.Unmarshal(payload, rule); err != nil {
			return nil, err
		}
		e.TaskTitle = rule.TaskTitle
		e.DueAt = timestamppb.New(due)
		out = append(out, e)
	}
	return out, rows.Err()
}
