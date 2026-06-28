package search

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

// SyncStats summarises a single SyncOnce pass.
type SyncStats struct {
	Seen     int
	Added    int
	Updated  int
	Touched  int // hash unchanged, metadata refreshed only
	Deleted  int
	Embedded int
	Errors   int
}

type Indexer struct {
	cfg     *Config
	pool    *pgxpool.Pool
	metrics *searchMetrics
}

func NewIndexer(cfg *Config, pool *pgxpool.Pool, metrics *searchMetrics) *Indexer {
	return &Indexer{cfg: cfg, pool: pool, metrics: metrics}
}

// SyncOnce walks the vault, reconciles the notes table, and (when enabled) the
// chunks/embeddings. Safe to call concurrently — Postgres upserts are atomic
// per row — but the caller is expected to serialize ticks.
func (ix *Indexer) SyncOnce(ctx context.Context) (SyncStats, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, logger)
	start := time.Now()

	var stats SyncStats
	seenSet := make(map[string]struct{}, 1024)

	walkErr := filepath.WalkDir(ix.cfg.NotesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warn("walk error", zap.String("path", path), zap.Error(err))
			stats.Errors++
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || slices.Contains(ix.cfg.IgnoreDirs, name) {
				if path != ix.cfg.NotesDir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(ix.cfg.NotesDir, path)
		if err != nil {
			stats.Errors++
			return nil
		}
		rel = filepath.ToSlash(rel)
		seenSet[rel] = struct{}{}
		stats.Seen++

		if err := ix.syncFile(ctx, path, rel, &stats); err != nil {
			log.Warn("sync file", zap.String("relpath", rel), zap.Error(err))
			stats.Errors++
		}
		return nil
	})
	if walkErr != nil {
		return stats, fmt.Errorf("walk vault: %w", walkErr)
	}

	known, err := AllRelpaths(ctx, ix.pool)
	if err != nil {
		log.Error("list known relpaths", zap.Error(err))
	} else {
		for _, rel := range known {
			if _, ok := seenSet[rel]; ok {
				continue
			}
			if err := DeleteNote(ctx, ix.pool, rel); err != nil {
				log.Warn("delete note", zap.String("relpath", rel), zap.Error(err))
				stats.Errors++
				continue
			}
			stats.Deleted++
		}
	}

	if ix.metrics != nil {
		ix.metrics.recordSync(ctx, stats, time.Since(start))
	}
	log.Info("sync done",
		zap.Int("seen", stats.Seen),
		zap.Int("added", stats.Added),
		zap.Int("updated", stats.Updated),
		zap.Int("touched", stats.Touched),
		zap.Int("deleted", stats.Deleted),
		zap.Int("embedded", stats.Embedded),
		zap.Int("errors", stats.Errors),
		zap.Duration("took", time.Since(start)),
	)
	return stats, nil
}

func (ix *Indexer) syncFile(ctx context.Context, fullPath, relpath string, stats *SyncStats) error {
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	existing, err := GetNoteMeta(ctx, ix.pool, relpath)
	if err != nil {
		return err
	}
	if existing != nil &&
		existing.Mtime.Unix() == info.ModTime().Unix() &&
		existing.Size == info.Size() {
		// File unchanged — skip read.
		return nil
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	hash := sha256Hash(data)

	if existing != nil && bytesEqual(existing.ContentHash, hash) {
		// Same content, only metadata drifted.
		if err := TouchNoteMeta(ctx, ix.pool, relpath, info.ModTime(), info.Size()); err != nil {
			return err
		}
		stats.Touched++
		return nil
	}

	name := strings.TrimSuffix(filepath.Base(relpath), filepath.Ext(relpath))
	full := NoteFull{
		NoteRow: NoteRow{
			Relpath:     relpath,
			Name:        name,
			Mtime:       info.ModTime(),
			Size:        info.Size(),
			ContentHash: hash,
		},
		Content: string(data),
	}
	_, inserted, err := UpsertNote(ctx, ix.pool, full)
	if err != nil {
		return err
	}
	if inserted {
		stats.Added++
	} else {
		stats.Updated++
	}
	return nil
}

func sha256Hash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
