package english

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed queries/*.sql migrations/*.sql
var sqlFiles embed.FS

func query(name string) string {
	text, err := sqlFiles.ReadFile("queries/" + name + ".sql")
	if err != nil {
		panic(err)
	}
	return string(text)
}

type Store struct {
	Pool *pgxpool.Pool
}

func (s Store) Migrate(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, query("migration_lock")); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, query("migration_ledger")); err != nil {
			return err
		}
		names, _ := fs.Glob(sqlFiles, "migrations/*.sql")
		for _, name := range names {
			var exists bool
			if err := tx.QueryRow(ctx, query("migration_exists"), name).Scan(&exists); err != nil {
				return err
			}
			if exists {
				continue
			}
			migration, _ := sqlFiles.ReadFile(name)
			if _, err := tx.Exec(ctx, string(migration)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, query("migration_record"), name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s Store) Check(ctx context.Context) error {
	names, _ := fs.Glob(sqlFiles, "migrations/*.sql")
	for _, name := range names {
		var exists bool
		if err := s.Pool.QueryRow(ctx, query("migration_exists"), name).Scan(&exists); err != nil || !exists {
			return errors.New("review database schema unavailable")
		}
	}
	var imported bool
	if err := s.Pool.QueryRow(ctx, query("check_import")).Scan(&imported); err != nil || !imported {
		return errors.New("legacy progress migration required")
	}
	return nil
}

func loadSession(ctx context.Context, tx pgx.Tx) (session, bool, error) {
	var data, visibility []byte
	var imported bool
	if err := tx.QueryRow(ctx, query("load")).Scan(&data, &imported, &visibility); err != nil {
		return session{}, false, err
	}
	state, err := decodeSession(data, visibility)
	return state, imported, err
}

func (s Store) readSession(ctx context.Context) (session, error) {
	var data, visibility []byte
	if err := s.Pool.QueryRow(ctx, query("read_progress")).Scan(&data, &visibility); err != nil {
		return session{}, err
	}
	return decodeSession(data, visibility)
}

func decodeSession(data, visibility []byte) (session, error) {
	var state session
	if err := json.Unmarshal(data, &state); err != nil {
		return session{}, err
	}
	if err := json.Unmarshal(visibility, &state.cardVisibility); err != nil {
		return session{}, err
	}
	if state.Progress == nil {
		state.Progress = make(map[string]progress)
	}
	return state, nil
}

func saveSession(ctx context.Context, tx pgx.Tx, state session, imported bool) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, query("update"), data, imported)
	return err
}
