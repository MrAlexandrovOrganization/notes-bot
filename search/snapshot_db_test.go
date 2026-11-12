package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmptySnapshotCannotDeleteTheExistingIndex(t *testing.T) {
	dsn := os.Getenv("TEST_SEARCH_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated pgvector fixture")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := EnsureSchema(ctx, pool, FeatureConfig{}, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE notes CASCADE"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	first, empty := filepath.Join(root, "first"), filepath.Join(root, "empty")
	for _, path := range []string{first, empty} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, "synthetic.md"), []byte("Synthetic searchable note"), 0o600); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root, "current")
	if err := os.Symlink(first, current); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".snapshot.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	indexer := NewIndexer(&Config{NotesDir: current, SnapshotLockFile: lock}, pool, nil, nil, nil)
	if _, err := indexer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(empty, current); err != nil {
		t.Fatal(err)
	}
	if _, err := indexer.SyncOnce(ctx); err == nil {
		t.Fatal("empty mirror was accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM notes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("last complete index was lost: count=%d err=%v", count, err)
	}
}
