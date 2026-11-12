package automations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
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
			return fmt.Errorf("run make migrate-automations before enabling automations")
		}
	}
	return nil
}
