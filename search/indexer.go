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
	"sync"
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
	syncMu   sync.Mutex
}

func NewIndexer(cfg *Config, pool *pgxpool.Pool, metrics *searchMetrics, embedder *Embedder) *Indexer {
	return &Indexer{cfg: cfg, pool: pool, metrics: metrics, embedder: embedder}
}

// SyncOnce reconciles changed files and resumes any stale/missing chunk index.
func (ix *Indexer) SyncOnce(ctx context.Context) (SyncStats, error) {
	return ix.syncOnce(ctx, false)
}

// ForceReindex reparses and re-embeds every note even if the source file hash
// is unchanged. Calls are serialized with scheduled syncs.
func (ix *Indexer) ForceReindex(ctx context.Context) (SyncStats, error) {
	return ix.syncOnce(ctx, true)
}

func (ix *Indexer) syncOnce(ctx context.Context, force bool) (SyncStats, error) {
	ix.syncMu.Lock()
	defer ix.syncMu.Unlock()

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

		if err := ix.syncFile(ctx, path, rel, force, &stats); err != nil {
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

func (ix *Indexer) syncFile(ctx context.Context, fullPath, relpath string, force bool, stats *SyncStats) error {
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	existing, err := GetNoteMeta(ctx, ix.pool, relpath)
	if err != nil {
		return err
	}
	if !force && existing != nil &&
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

	if !force && existing != nil && bytesEqual(existing.ContentHash, hash) {
		// Same content, only metadata drifted.
		if err := TouchNoteMeta(ctx, ix.pool, relpath, info.ModTime(), info.Size()); err != nil {
			return err
		}
		stats.Touched++
		return nil
	}

	name := strings.TrimSuffix(filepath.Base(relpath), filepath.Ext(relpath))
	doc := ParseDocument(string(data), name)
	full := NoteFull{
		NoteRow: NoteRow{
			Relpath:     relpath,
			Name:        name,
			Mtime:       info.ModTime(),
			Size:        info.Size(),
			ContentHash: hash,
		},
		Content:  string(data),
		Body:     doc.Body,
		Metadata: doc.Metadata,
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
		source := "sync"
		if force {
			source = "force"
		}
		embedded, err := ix.reindexChunks(ctx, noteID, name, doc, force, source)
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
func (ix *Indexer) reindexChunks(ctx context.Context, noteID int64, noteName string, doc ParsedDocument, force bool, source string) (embedded int, err error) {
	started := time.Now()
	defer func() {
		ix.metrics.recordIndexNote(ctx, source, embedded, time.Since(started), err)
	}()

	chunks := ChunkContent(doc.Body)

	existing, err := ListChunkHashes(ctx, ix.pool, noteID)
	if err != nil {
		return 0, err
	}
	existingByKey := make(map[string]ChunkRow, len(existing))
	for _, c := range existing {
		existingByKey[chunkKey(c.Kind, c.Ord)] = c
	}

	keepKeys := make(map[string]struct{}, len(chunks))
	for _, c := range chunks {
		keepKeys[chunkKey(string(c.Kind), c.Ord)] = struct{}{}
	}
	staleIDs := make([]int64, 0)
	for key, row := range existingByKey {
		if _, keep := keepKeys[key]; !keep {
			staleIDs = append(staleIDs, row.ID)
		}
	}

	// Identify chunks that need embedding (new or changed hash).
	type pending struct {
		idx   int
		hash  []byte
		input string
	}
	var todo []pending
	for i, c := range chunks {
		input := embeddingInput(noteName, doc.Metadata, c)
		h := sha256Hash([]byte(input))
		if prev, ok := existingByKey[chunkKey(string(c.Kind), c.Ord)]; !force && ok && bytesEqual(prev.ChunkHash, h) {
			continue
		}
		todo = append(todo, pending{idx: i, hash: h, input: input})
	}

	// Keep both the number of inputs and their total size bounded. Ollama's CPU
	// backend processes batch members sequentially, so a large count can exceed
	// the HTTP timeout even when every individual input fits the model context.
	const (
		maxBatchInputs = 8
		maxBatchRunes  = 4000
	)
	for start := 0; start < len(todo); {
		end := start
		batchRunes := 0
		for end < len(todo) && end-start < maxBatchInputs {
			n := len([]rune(todo[end].input))
			if end > start && batchRunes+n > maxBatchRunes {
				break
			}
			batchRunes += n
			end++
		}
		batch := todo[start:end]
		inputs := make([]string, len(batch))
		for i, p := range batch {
			inputs[i] = p.input
		}
		vecs, err := ix.embedder.Embed(ctx, inputs, ix.metrics)
		if err != nil {
			return 0, fmt.Errorf("embed batch: %w", err)
		}
		for i, p := range batch {
			c := chunks[p.idx]
			if err := UpsertChunk(ctx, ix.pool, noteID, string(c.Kind), c.Ord, c.Text, c.HeadingPath, p.hash, vecs[i], ix.cfg.EmbedModel); err != nil {
				return embedded, err
			}
		}
		embedded += len(batch)
		start = end
	}
	if _, err := DeleteChunksByID(ctx, ix.pool, staleIDs); err != nil {
		return embedded, err
	}
	if err := MarkNoteChunksCurrent(ctx, ix.pool, noteID, ix.cfg.EmbedModel); err != nil {
		return embedded, err
	}
	return embedded, nil
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
	var afterID int64

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

		notes, err := NotesNeedingChunks(ctx, ix.pool, afterID, page, ix.cfg.EmbedModel)
		if err != nil {
			return embedded, err
		}
		if len(notes) == 0 {
			break
		}

		for _, n := range notes {
			afterID = n.ID
			doc := ParseDocument(n.Content, n.Name)
			if err := UpdateNoteParsed(ctx, ix.pool, n.ID, doc); err != nil {
				return embedded, err
			}
			emb, err := ix.reindexChunks(ctx, n.ID, n.Name, doc, true, "backfill")
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
