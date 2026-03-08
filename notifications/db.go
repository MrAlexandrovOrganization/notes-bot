package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const createTableSQL = `
CREATE TABLE IF NOT EXISTS reminders (
    id SERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    title TEXT NOT NULL,
    schedule_type TEXT NOT NULL,
    schedule_params JSONB NOT NULL DEFAULT '{}',
    next_fire_at TIMESTAMPTZ NOT NULL,
    last_fired_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    create_task BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_reminders_next_fire ON reminders (next_fire_at) WHERE is_active = TRUE;
`

const migrateSQL = `ALTER TABLE reminders ADD COLUMN IF NOT EXISTS create_task BOOLEAN NOT NULL DEFAULT FALSE;`

// Reminder represents a row from the reminders table.
type Reminder struct {
	ID             int64
	UserID         int64
	Title          string
	ScheduleType   string
	ScheduleParams map[string]any
	NextFireAt     time.Time
	IsActive       bool
	CreateTask     bool
}

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	_, err = pool.Exec(ctx, migrateSQL)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Info("database schema ensured")
	return nil
}

func CreateReminder(ctx context.Context, pool *pgxpool.Pool,
	userID int64, title, scheduleType string,
	params map[string]any, nextFireAt time.Time, createTask bool,
) (*Reminder, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	row := pool.QueryRow(ctx, `
		INSERT INTO reminders (user_id, title, schedule_type, schedule_params, next_fire_at, create_task)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
	`, userID, title, scheduleType, paramsJSON, nextFireAt, createTask)

	return scanReminder(row)
}

func ListReminders(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]*Reminder, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
		FROM reminders
		WHERE user_id = $1 AND is_active = TRUE
		ORDER BY next_fire_at ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var result []*Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func DeleteReminder(ctx context.Context, pool *pgxpool.Pool, reminderID, userID int64) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE reminders SET is_active = FALSE
		WHERE id = $1 AND user_id = $2 AND is_active = TRUE
	`, reminderID, userID)
	if err != nil {
		return false, fmt.Errorf("delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func GetDueReminders(ctx context.Context, pool *pgxpool.Pool) ([]*Reminder, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		SELECT id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
		FROM reminders
		WHERE is_active = TRUE AND next_fire_at <= NOW()
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		return nil, fmt.Errorf("query due: %w", err)
	}

	var result []*Reminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return result, nil
}

func UpdateNextFire(ctx context.Context, pool *pgxpool.Pool, reminderID int64, nextFireAt *time.Time) error {
	var err error
	if nextFireAt == nil {
		_, err = pool.Exec(ctx,
			"UPDATE reminders SET is_active = FALSE, last_fired_at = NOW() WHERE id = $1",
			reminderID)
	} else {
		_, err = pool.Exec(ctx,
			"UPDATE reminders SET next_fire_at = $1, last_fired_at = NOW() WHERE id = $2",
			*nextFireAt, reminderID)
	}
	if err != nil {
		return fmt.Errorf("update next fire: %w", err)
	}
	return nil
}

func SetNextFireAt(ctx context.Context, pool *pgxpool.Pool, reminderID, userID int64, nextFireAt time.Time) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE reminders SET next_fire_at = $1
		WHERE id = $2 AND user_id = $3 AND is_active = TRUE
	`, nextFireAt, reminderID, userID)
	if err != nil {
		return false, fmt.Errorf("set next fire: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReminder(row rowScanner) (*Reminder, error) {
	var r Reminder
	var paramsJSON []byte

	if err := row.Scan(
		&r.ID, &r.UserID, &r.Title, &r.ScheduleType,
		&paramsJSON, &r.NextFireAt, &r.IsActive, &r.CreateTask,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan: %w", err)
	}

	if err := json.Unmarshal(paramsJSON, &r.ScheduleParams); err != nil {
		logger.Warn("failed to unmarshal params", zap.Error(err))
		r.ScheduleParams = map[string]any{}
	}
	return &r, nil
}
