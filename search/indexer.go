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
	cfg      *Config
	pool     *pgxpool.Pool
	metrics  *searchMetrics
	embedder *Embedder
}

func NewIndexer(cfg *Config, pool *pgxpool.Pool, metrics *searchMetrics, embedder *Embedder) *Indexer {
	return &Indexer{cfg: cfg, pool: pool, metrics: metrics, embedder: embedder}
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

	if ix.cfg.EnableEmbeddings && ix.embedder != nil {
		if n, err := ix.backfillChunks(ctx); err != nil {
			log.Warn("backfill chunks", zap.Error(err))
			stats.Errors++
		} else {
			stats.Embedded += n
		}
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
	noteID, inserted, err := UpsertNote(ctx, ix.pool, full)
	if err != nil {
		return err
	}
	if inserted {
		stats.Added++
	} else {
		stats.Updated++
	}

	if ix.cfg.EnableEmbeddings && ix.embedder != nil {
		embedded, err := ix.reindexChunks(ctx, noteID, string(data))
		if err != nil {
			return fmt.Errorf("reindex chunks: %w", err)
		}
		stats.Embedded += embedded
	}
	return nil
}

// reindexChunks chunks the note, computes per-chunk hashes, embeds only
// new/changed chunks, and drops any stale ones. Returns how many chunks were
// embedded in this pass.
func (ix *Indexer) reindexChunks(ctx context.Context, noteID int64, content string) (int, error) {
	chunks := ChunkContent(content)

	existing, err := ListChunkHashes(ctx, ix.pool, noteID)
	if err != nil {
		return 0, err
	}
	existingByKey := make(map[string][]byte, len(existing))
	for _, c := range existing {
		existingByKey[chunkKey(c.Kind, c.Ord)] = c.ChunkHash
	}

	// Group chunks by kind so we can drop stale ords per kind separately.
	keepOrds := map[string][]int{}
	for _, c := range chunks {
		keepOrds[string(c.Kind)] = append(keepOrds[string(c.Kind)], c.Ord)
	}
	for _, kind := range []string{string(KindNote), string(KindParagraph), string(KindTask)} {
		if _, err := DeleteChunksOutsideOrd(ctx, ix.pool, noteID, kind, keepOrds[kind]); err != nil {
			return 0, err
		}
	}

	// Identify chunks that need embedding (new or changed hash).
	type pending struct {
		idx  int
		hash []byte
	}
	var todo []pending
	hashes := make([][]byte, len(chunks))
	for i, c := range chunks {
		h := sha256Hash([]byte(c.Text))
		hashes[i] = h
		if prev, ok := existingByKey[chunkKey(string(c.Kind), c.Ord)]; ok && bytesEqual(prev, h) {
			continue
		}
		todo = append(todo, pending{idx: i, hash: h})
	}
	if len(todo) == 0 {
		return 0, nil
	}

	// Batch embed in chunks to keep request sizes reasonable.
	const batchSize = 32
	for start := 0; start < len(todo); start += batchSize {
		end := min(start+batchSize, len(todo))
		batch := todo[start:end]
		inputs := make([]string, len(batch))
		for i, p := range batch {
			inputs[i] = chunks[p.idx].Text
		}
		vecs, err := ix.embedder.Embed(ctx, inputs, ix.metrics)
		if err != nil {
			return 0, fmt.Errorf("embed batch: %w", err)
		}
		for i, p := range batch {
			c := chunks[p.idx]
			if err := UpsertChunk(ctx, ix.pool, noteID, string(c.Kind), c.Ord, c.Text, p.hash, vecs[i]); err != nil {
				return 0, err
			}
		}
	}
	return len(todo), nil
}

func chunkKey(kind string, ord int) string {
	return fmt.Sprintf("%s/%d", kind, ord)
}

// backfillChunks finds notes without chunk rows and embeds them. The DB query
// is paged so a single pass with cfg.BackfillBatchPerPass=0 drains the entire
// backlog without loading everything into memory at once.
const backfillPageSize = 200

func (ix *Indexer) backfillChunks(ctx context.Context) (int, error) {
	log := applog.With(ctx, logger)

	limit := ix.cfg.BackfillBatchPerPass
	processed := 0
	embedded := 0

	for {
		if ctx.Err() != nil {
			return embedded, ctx.Err()
		}
		page := backfillPageSize
		if limit > 0 {
			remaining := limit - processed
			if remaining <= 0 {
				break
			}
			page = min(page, remaining)
		}

		notes, err := NotesMissingChunks(ctx, ix.pool, page)
		if err != nil {
			return embedded, err
		}
		if len(notes) == 0 {
			break
		}

		for _, n := range notes {
			emb, err := ix.reindexChunks(ctx, n.ID, n.Content)
			if err != nil {
				return embedded, err
			}
			embedded += emb
			processed++
			if processed%100 == 0 {
				log.Info("backfill progress",
					zap.Int("notes_processed", processed),
					zap.Int("chunks_embedded", embedded),
				)
			}
		}
	}
	if processed > 0 {
		log.Info("backfill pass done",
			zap.Int("notes_processed", processed),
			zap.Int("chunks_embedded", embedded),
		)
	}
	return embedded, nil
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
