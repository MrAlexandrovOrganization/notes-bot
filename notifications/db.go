package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"notes-bot/internal/telemetry"
	"time"

	"github.com/exaring/otelpgx"
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

const createLocationTableSQL = `
CREATE TABLE IF NOT EXISTS location_history (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    accuracy DOUBLE PRECISION,
    altitude DOUBLE PRECISION,
    heading DOUBLE PRECISION,
    speed DOUBLE PRECISION,
    source TEXT NOT NULL,
    live_message_id BIGINT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_location_user_time ON location_history (user_id, recorded_at DESC);
`

const createTrackingTableSQL = `
CREATE TABLE IF NOT EXISTS location_tracking (
    user_id BIGINT PRIMARY KEY,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
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

type LocationRecord struct {
	ID            int64
	UserID        int64
	Latitude      float64
	Longitude     float64
	Accuracy      *float64
	Altitude      *float64
	Heading       *float64
	Speed         *float64
	Source        string
	LiveMessageID *int64
	RecordedAt    time.Time
}

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	config.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	_, err = pool.Exec(ctx, migrateSQL)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	_, err = pool.Exec(ctx, createLocationTableSQL)
	if err != nil {
		return fmt.Errorf("create location table: %w", err)
	}
	_, err = pool.Exec(ctx, createTrackingTableSQL)
	if err != nil {
		return fmt.Errorf("create tracking table: %w", err)
	}
	logger.Info("database schema ensured")
	return nil
}

func CreateReminder(ctx context.Context, pool *pgxpool.Pool,
	userID int64, title, scheduleType string,
	params map[string]any, nextFireAt time.Time, createTask bool,
) (*Reminder, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	row := pool.QueryRow(ctx, `
		INSERT INTO reminders (user_id, title, schedule_type, schedule_params, next_fire_at, create_task)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, title, schedule_type, schedule_params, next_fire_at, is_active, create_task
	`, userID, title, scheduleType, paramsJSON, nextFireAt, createTask)

	return scanReminder(ctx, row)
}

func ListReminders(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]*Reminder, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

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
		r, err := scanReminder(ctx, rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func DeleteReminder(ctx context.Context, pool *pgxpool.Pool, reminderID, userID int64) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

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
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

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
		r, err := scanReminder(ctx, rows)
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
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

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
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	tag, err := pool.Exec(ctx, `
		UPDATE reminders SET next_fire_at = $1, is_active = TRUE
		WHERE id = $2 AND user_id = $3
	`, nextFireAt, reminderID, userID)
	if err != nil {
		return false, fmt.Errorf("set next fire: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func CountActiveReminders(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM reminders WHERE is_active = TRUE").Scan(&count)
	return count, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReminder(ctx context.Context, row rowScanner) (*Reminder, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

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

func StoreLocation(ctx context.Context, pool *pgxpool.Pool, loc *LocationRecord) (*LocationRecord, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	row := pool.QueryRow(ctx, `
		INSERT INTO location_history (user_id, latitude, longitude, accuracy, altitude, heading, speed, source, live_message_id, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, user_id, latitude, longitude, accuracy, altitude, heading, speed, source, live_message_id, recorded_at
	`, loc.UserID, loc.Latitude, loc.Longitude, loc.Accuracy, loc.Altitude, loc.Heading, loc.Speed, loc.Source, loc.LiveMessageID, loc.RecordedAt)

	return scanLocation(ctx, row)
}

func GetLatestLocation(ctx context.Context, pool *pgxpool.Pool, userID int64) (*LocationRecord, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	row := pool.QueryRow(ctx, `
		SELECT id, user_id, latitude, longitude, accuracy, altitude, heading, speed, source, live_message_id, recorded_at
		FROM location_history
		WHERE user_id = $1
		ORDER BY recorded_at DESC
		LIMIT 1
	`, userID)

	return scanLocation(ctx, row)
}

func GetLocationHistory(ctx context.Context, pool *pgxpool.Pool, userID int64, limit, offset int) ([]*LocationRecord, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	rows, err := pool.Query(ctx, `
		SELECT id, user_id, latitude, longitude, accuracy, altitude, heading, speed, source, live_message_id, recorded_at
		FROM location_history
		WHERE user_id = $1
		ORDER BY recorded_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var result []*LocationRecord
	for rows.Next() {
		loc, err := scanLocation(ctx, rows)
		if err != nil {
			return nil, err
		}
		result = append(result, loc)
	}
	return result, rows.Err()
}

func CountLocationUpdates(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM location_history").Scan(&count)
	return count, err
}

func SetLocationTracking(ctx context.Context, pool *pgxpool.Pool, userID int64, active bool) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, `
		INSERT INTO location_tracking (user_id, is_active, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE SET is_active = $2, updated_at = NOW()
	`, userID, active)
	return err
}

func GetLocationTracking(ctx context.Context, pool *pgxpool.Pool, userID int64) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	var isActive bool
	err := pool.QueryRow(ctx, `
		SELECT is_active FROM location_tracking WHERE user_id = $1
	`, userID).Scan(&isActive)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return isActive, err
}

func CountActiveLocationTracking(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM location_tracking WHERE is_active = TRUE").Scan(&count)
	return count, err
}

func scanLocation(ctx context.Context, row rowScanner) (*LocationRecord, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	var loc LocationRecord
	err := row.Scan(
		&loc.ID, &loc.UserID, &loc.Latitude, &loc.Longitude,
		&loc.Accuracy, &loc.Altitude, &loc.Heading, &loc.Speed,
		&loc.Source, &loc.LiveMessageID, &loc.RecordedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan location: %w", err)
	}
	return &loc, nil
}
