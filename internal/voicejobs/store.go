// Package voicejobs stores accepted voice input and delivery progress independently
// of Telegram sessions and the transcription service's result retention.
package voicejobs

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql migrations/*.sql
var files embed.FS

func query(name string) string {
	b, err := files.ReadFile("sql/" + name + ".sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

type Job struct {
	ID              string          `json:"id"`
	ChatID          int64           `json:"chat_id"`
	MessageID       int             `json:"message_id"`
	UserID          int64           `json:"user_id"`
	FileID          string          `json:"file_id"`
	Format          string          `json:"format"`
	ActiveDate      string          `json:"active_date"`
	ActiveRelpath   string          `json:"active_relpath,omitempty"`
	Mode            string          `json:"mode"`
	SentAt          time.Time       `json:"sent_at"`
	TimezoneOffset  int             `json:"timezone_offset"`
	DayStartHour    int             `json:"day_start_hour"`
	Traceparent     string          `json:"traceparent,omitempty"`
	Tracestate      string          `json:"tracestate,omitempty"`
	StatusMessageID int             `json:"status_message_id"`
	RemoteID        string          `json:"remote_id,omitempty"`
	SubmitStarted   bool            `json:"submit_started"`
	Text            string          `json:"text,omitempty"`
	Error           string          `json:"error,omitempty"`
	ResultReady     bool            `json:"result_ready"`
	Acked           bool            `json:"acked"`
	NoteSaved       bool            `json:"note_saved"`
	Prepared        json.RawMessage `json:"prepared,omitempty"`
	LastStatus      string          `json:"last_status,omitempty"`
	CancelRequested bool            `json:"-"`
	LeaseToken      string          `json:"-"`
}

var ErrLeaseLost = errors.New("voice job lease lost")

type Repository interface {
	Enqueue(context.Context, *Job) error
	Find(context.Context, int64, int) (*Job, error)
	Claim(context.Context) (*Job, error)
	Save(context.Context, *Job) error
	Release(context.Context, *Job, bool, time.Duration) error
	IsFront(context.Context, *Job) (bool, error)
	Cancel(context.Context, string, int64) error
	Get(context.Context, string, int64) (*Job, error)
	Page(context.Context, int64, int64, int) (*Job, error)
}

type Store struct{ Pool *pgxpool.Pool }

// Migrate is an explicit deployment operation, never called by normal startup.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err = tx.Exec(ctx, query("migration_lock")); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, query("migration_ledger")); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRow(ctx, query("migration_exists")).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		b, e := files.ReadFile("migrations/001.sql")
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, string(b)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, query("migration_record")); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CheckSchema(ctx context.Context) error {
	var version int
	if err := s.Pool.QueryRow(ctx, query("migration_version")).Scan(&version); err != nil {
		return errors.New("voice schema unavailable; run make migrate-voice")
	}
	if version != 1 {
		return errors.New("unsupported voice schema version")
	}
	return nil
}
func (s *Store) Enqueue(ctx context.Context, j *Job) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, query("enqueue"), j.ID, j.ChatID, j.MessageID, j.UserID, b)
	return err
}
func scan(row pgx.Row) (*Job, error) {
	var b []byte
	var cancel bool
	if err := row.Scan(&b, &cancel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	j.CancelRequested = cancel
	return &j, nil
}
func (s *Store) Find(ctx context.Context, chat int64, msg int) (*Job, error) {
	return scan(s.Pool.QueryRow(ctx, query("find"), chat, msg))
}
func (s *Store) Get(ctx context.Context, id string, user int64) (*Job, error) {
	return scan(s.Pool.QueryRow(ctx, query("get"), id, user))
}
func (s *Store) Page(ctx context.Context, chat, user int64, msg int) (*Job, error) {
	return scan(s.Pool.QueryRow(ctx, query("page"), chat, user, msg))
}
func (s *Store) Claim(ctx context.Context) (*Job, error) {
	token := uuid.NewString()
	j, err := scan(s.Pool.QueryRow(ctx, query("claim"), token))
	if j != nil {
		j.LeaseToken = token
	}
	return j, err
}
func (s *Store) Save(ctx context.Context, j *Job) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, query("save"), j.ID, j.LeaseToken, b)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (s *Store) Release(ctx context.Context, j *Job, done bool, delay time.Duration) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, query("release"), j.ID, j.LeaseToken, b, done, delay.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (s *Store) IsFront(ctx context.Context, j *Job) (bool, error) {
	var front bool
	err := s.Pool.QueryRow(ctx, query("front"), j.ChatID, j.MessageID).Scan(&front)
	return front, err
}
func (s *Store) Cancel(ctx context.Context, id string, user int64) error {
	_, err := s.Pool.Exec(ctx, query("cancel"), id, user)
	return err
}

// SetPrepared commits an explicitly requested refinement of a delivered smart
// preview. It cannot overwrite active processing or another callback's result.
func (s *Store) SetPrepared(ctx context.Context, id string, user int64, oldPrepared, newPrepared json.RawMessage) (bool, error) {
	tag, err := s.Pool.Exec(ctx, query("prepared"), id, user, []byte(oldPrepared), []byte(newPrepared))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// NewPool builds connection settings without interpolating a password into a DSN.
func NewPool(ctx context.Context, host, port, name, user, password string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, err
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil || p == 0 {
		return nil, errors.New("invalid DB_PORT")
	}
	cfg.ConnConfig.Host = host
	cfg.ConnConfig.Port = uint16(p)
	cfg.ConnConfig.Database = name
	cfg.ConnConfig.User = user
	cfg.ConnConfig.Password = password
	cfg.MaxConns = 4
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("voice database configuration failed")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("voice database unavailable")
	}
	return pool, nil
}
