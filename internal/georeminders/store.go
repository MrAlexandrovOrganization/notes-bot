package georeminders

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

//go:embed queries/*.sql migrations/*.sql
var files embed.FS

func query(name string) string {
	b, err := files.ReadFile("queries/" + name + ".sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

type Store struct{ Pool *pgxpool.Pool }

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, query("migration_lock")); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, query("migration_init")); err != nil {
		return err
	}
	names, _ := fs.Glob(files, "migrations/*.sql")
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err = tx.QueryRow(ctx, query("migration_exists"), name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		b, _ := files.ReadFile(name)
		if _, err = tx.Exec(ctx, string(b)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, query("migration_record"), name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) Check(ctx context.Context) error {
	names, _ := fs.Glob(files, "migrations/*.sql")
	for _, name := range names {
		var exists bool
		if err := s.Pool.QueryRow(ctx, query("migration_exists"), name).Scan(&exists); err != nil || !exists {
			return fmt.Errorf("run make migrate-geo before enabling geofences")
		}
	}
	return nil
}
func (s *Store) Create(ctx context.Context, user int64, r *pb.Rule, requestID string) error {
	// Serialize the bounded create operation across processes.
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, query("migration_lock")); err != nil {
		return err
	}
	var at *time.Time
	if err = tx.QueryRow(ctx, query("create_rule"), user, r.PlaceId, r.Title, r.Transition, r.Repeat, r.MaxAgeSeconds, r.Timezone, requestID).Scan(&r.Id, &r.Armed, &at); err != nil {
		return err
	}
	if at != nil {
		r.LastFiredAt = timestamppb.New(*at)
	}
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
		r := new(pb.Rule)
		var at *time.Time
		if err = rows.Scan(&r.Id, &r.PlaceId, &r.Title, &r.Transition, &r.Repeat, &r.MaxAgeSeconds, &r.Timezone, &r.Armed, &at); err != nil {
			return nil, err
		}
		if at != nil {
			r.LastFiredAt = timestamppb.New(*at)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Cursor(ctx context.Context) (int64, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, query("cursor_read")).Scan(&n)
	return n, err
}

// Apply advances the feed cursor in the same transaction as rule executions.
func (s *Store) Apply(ctx context.Context, expected int64, feed *loc.ListEventsResponse, places map[string]*loc.Place, now time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actual int64
	if err = tx.QueryRow(ctx, query("cursor")).Scan(&actual); err != nil {
		return err
	}
	if actual != expected {
		return nil
	}
	for _, e := range feed.Events {
		p := places[e.PlaceId]
		if p == nil || p.Version != e.PlaceVersion || e.OccurredAt == nil || e.ConfirmedAt == nil {
			continue
		}
		at := e.ConfirmedAt.AsTime()
		if at.After(now) || now.Sub(at) > time.Hour {
			continue
		}
		rows, err := tx.Query(ctx, query("matching_rules"), e.PlaceId, e.Transition, e.OccurredAt.AsTime(), at)
		if err != nil {
			return err
		}
		type match struct {
			id, user int64
			title    string
			age      int32
		}
		var matches []match
		for rows.Next() {
			var m match
			if err = rows.Scan(&m.id, &m.user, &m.title, &m.age); err != nil {
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
			expiry := at.Add(time.Duration(m.age) * time.Second)
			if !expiry.After(now) {
				continue
			}
			if _, err = tx.Exec(ctx, query("enqueue"), m.id, e.Sequence, m.user, m.title, p.Name, e.Transition, expiry); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, query("fired"), m.id, at); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, query("cursor_save"), feed.NextSequence); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Claim(ctx context.Context, user int64) (*pb.Delivery, error) {
	if _, err := s.Pool.Exec(ctx, query("expire")); err != nil {
		return nil, err
	}
	d := new(pb.Delivery)
	var expiry time.Time
	err := s.Pool.QueryRow(ctx, query("claim"), user, uuid.NewString()).Scan(&d.Id, &d.LeaseToken, &d.Title, &d.PlaceName, &d.Transition, &expiry)
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
