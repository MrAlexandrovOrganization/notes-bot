// Package mobilevoice manages audio submitted by the native Notes clients.
package mobilevoice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql migrations/*.sql
var sqlFiles embed.FS

const MaxAudioBytes = 16 << 20

const (
	Queued       = "queued"
	Transcribing = "transcribing"
	Saved        = "saved"
	Failed       = "failed"
)

var (
	ErrNotFound  = errors.New("mobile voice job not found")
	ErrConflict  = errors.New("mobile voice job does not match upload")
	ErrQueueFull = errors.New("mobile voice queue is full")
	errLeaseLost = errors.New("mobile voice lease lost")
)

type Status struct {
	ID    string `json:"id"`
	State string `json:"status"`
	Error string `json:"error,omitempty"`
}

type Upload struct {
	ID    string
	Date  string
	Path  string
	Audio []byte
}

type Job struct {
	Status
	UploadDate  string
	UploadPath  string
	Audio       []byte
	RemoteID    string
	Transcript  string
	ResultReady bool
	Lease       string
}

type Store struct{ Pool *pgxpool.Pool }

func sql(name string) string {
	b, err := sqlFiles.ReadFile(name + ".sql")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, sql("sql/migration_lock")); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sql("sql/migrate")); err != nil {
		return err
	}
	var version int
	if err := tx.QueryRow(ctx, sql("sql/migration_version")).Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		if _, err := tx.Exec(ctx, sql("migrations/001")); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql("sql/migration_record")); err != nil {
			return err
		}
	case 1:
	default:
		return errors.New("unsupported mobile voice schema version")
	}
	return tx.Commit(ctx)
}

func (s *Store) CheckSchema(ctx context.Context) error {
	var version int
	if s.Pool.QueryRow(ctx, sql("sql/migration_version")).Scan(&version) != nil || version != 1 {
		return errors.New("mobile voice schema unavailable; run make migrate-voice")
	}
	return nil
}

func (s *Store) Enqueue(ctx context.Context, upload Upload) (Status, bool, error) {
	digest := sha256.Sum256(upload.Audio)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Status{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialize admissions across replicas so the pending row limit bounds the
	// amount of audio the database has to hold while Transcriber is unreachable.
	if _, err := tx.Exec(ctx, sql("sql/enqueue_lock")); err != nil {
		return Status{}, false, err
	}
	var result Status
	var date, path string
	var storedHash []byte
	err = tx.QueryRow(ctx, sql("sql/find"), upload.ID).
		Scan(&result.ID, &result.State, &result.Error, &date, &path, &storedHash)
	switch {
	case err == nil:
		if date != upload.Date || path != upload.Path || !bytes.Equal(storedHash, digest[:]) {
			return Status{}, false, ErrConflict
		}
		return result, false, tx.Commit(ctx)
	case !errors.Is(err, pgx.ErrNoRows):
		return Status{}, false, err
	}

	err = tx.QueryRow(ctx, sql("sql/enqueue"), upload.ID, upload.Date, upload.Path, digest[:], upload.Audio).
		Scan(&result.ID, &result.State, &result.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, false, ErrQueueFull
	}
	if err != nil {
		return Status{}, false, err
	}
	return result, true, tx.Commit(ctx)
}

func (s *Store) Get(ctx context.Context, id string) (Status, error) {
	var result Status
	err := s.Pool.QueryRow(ctx, sql("sql/get"), id).Scan(&result.ID, &result.State, &result.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return result, err
}

func (s *Store) Claim(ctx context.Context) (*Job, error) {
	job := &Job{Lease: uuid.NewString()}
	err := s.Pool.QueryRow(ctx, sql("sql/claim"), job.Lease).Scan(
		&job.ID, &job.State, &job.Error, &job.UploadDate, &job.UploadPath,
		&job.Audio, &job.RemoteID, &job.Transcript, &job.ResultReady)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func (s *Store) Release(ctx context.Context, job *Job, delay time.Duration) error {
	next := pgtype.Interval{Microseconds: delay.Microseconds(), Valid: true}
	tag, err := s.Pool.Exec(ctx, sql("sql/release"), job.ID, job.Lease,
		job.State, job.RemoteID, job.Transcript, job.Error, job.ResultReady, next)
	if err == nil && tag.RowsAffected() != 1 {
		return errLeaseLost
	}
	return err
}
