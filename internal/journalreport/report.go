package journalreport

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const refreshInterval = 15 * time.Minute

//go:embed queries/lock.sql
var lockSQL string

//go:embed queries/clear.sql
var clearSQL string

// Run keeps survey failures independent of the Notes Core service. The previous
// complete snapshot remains readable until a successful refresh commits.
func Run(ctx context.Context, vault, dsn string, logger *zap.Logger) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		refreshCtx, cancel := context.WithTimeout(ctx, time.Minute)
		snapshot, err := refresh(refreshCtx, vault, dsn)
		cancel()
		switch {
		case err == nil:
			logger.Info("journal report refreshed",
				zap.Int("days", len(snapshot.Days)),
				zap.Int("gym_days", len(snapshot.GymDays)),
				zap.Int("people_pairs_by_day", len(snapshot.Meetings)))
		case ctx.Err() == nil:
			// Filesystem errors may contain private filenames. Connection errors
			// may contain the credential or the full connection string.
			logger.Warn("journal report refresh failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func refresh(ctx context.Context, vault, dsn string) (Snapshot, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return Snapshot{}, errors.New("connect to journal database")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Snapshot{}, errors.New("begin journal transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, lockSQL); err != nil {
		return Snapshot{}, errors.New("lock journal refresh")
	}
	snapshot, err := Scan(ctx, vault)
	if err != nil {
		return Snapshot{}, errors.New("scan daily notes and links")
	}
	if err := replaceSnapshot(ctx, tx, snapshot); err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, errors.New("commit journal refresh")
	}
	return snapshot, nil
}

func replaceSnapshot(ctx context.Context, tx pgx.Tx, snapshot Snapshot) error {
	if _, err := tx.Exec(ctx, clearSQL); err != nil {
		return errors.New("clear previous journal snapshot")
	}
	copies := []struct {
		table   string
		columns []string
		rows    pgx.CopyFromSource
	}{
		{"daily", []string{"day", "rating", "characters", "links"},
			pgx.CopyFromSlice(len(snapshot.Days), func(i int) ([]any, error) {
				day := snapshot.Days[i]
				return []any{day.Date, day.Rating, day.Characters, day.Links}, nil
			})},
		{"gym", []string{"day"},
			pgx.CopyFromSlice(len(snapshot.GymDays), func(i int) ([]any, error) {
				return []any{snapshot.GymDays[i]}, nil
			})},
		{"people_pairs", []string{"day", "first_id", "first_name", "second_id", "second_name"},
			pgx.CopyFromSlice(len(snapshot.Meetings), func(i int) ([]any, error) {
				pair := snapshot.Meetings[i]
				return []any{pair.Date, pair.First.ID, pair.First.Name, pair.Second.ID, pair.Second.Name}, nil
			})},
	}
	for _, copy := range copies {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"journal", copy.table}, copy.columns, copy.rows); err != nil {
			return errors.New("write journal snapshot table " + copy.table)
		}
	}
	return nil
}
