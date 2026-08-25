package search

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

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
	Profiled int
	Errors   int
}

type Indexer struct {
	cfg      *Config
	pool     *pgxpool.Pool
	metrics  *searchMetrics
	embedder *Embedder
	profiles *ProfileExtractor
	syncMu   sync.Mutex
}

type vaultFile struct{ path, relpath string }

func NewIndexer(cfg *Config, pool *pgxpool.Pool, metrics *searchMetrics, embedder *Embedder, profiles *ProfileExtractor) *Indexer {
	return &Indexer{cfg: cfg, pool: pool, metrics: metrics, embedder: embedder, profiles: profiles}
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

func (ix *Indexer) syncOnce(ctx context.Context, force bool) (stats SyncStats, retErr error) {
	ix.syncMu.Lock()
	defer ix.syncMu.Unlock()

	ctx, span := telemetry.StartSpan(ctx, attribute.Bool("search.sync.force", force))
	defer func() {
		span.SetAttributes(
			attribute.Int("search.sync.files.seen", stats.Seen),
			attribute.Int("search.sync.files.added", stats.Added),
			attribute.Int("search.sync.files.updated", stats.Updated),
			attribute.Int("search.sync.files.touched", stats.Touched),
			attribute.Int("search.sync.files.deleted", stats.Deleted),
			attribute.Int("search.sync.chunks.embedded", stats.Embedded),
			attribute.Int("search.sync.notes.profiled", stats.Profiled),
			attribute.Int("search.sync.errors", stats.Errors),
		)
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(codes.Error, retErr.Error())
		} else if stats.Errors > 0 {
			span.SetStatus(codes.Error, "sync completed with item errors")
		}
		span.End()
	}()

	log := applog.With(ctx, logger)
	start := time.Now()
	if force && ix.cfg.Features.Profiles {
		if err := InvalidateNoteProfiles(ctx, ix.pool); err != nil {
			return stats, err
		}
	}

	var known map[string]*NoteRow
	var files []vaultFile
	var walkErrorCount int
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		known, err = AllNoteMeta(gctx, ix.pool)
		if err != nil {
			return fmt.Errorf("load note metadata: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		files, walkErrorCount, err = ix.discoverFiles(gctx, log, &stats)
		return err
	})
	if err := g.Wait(); err != nil {
		return stats, err
	}
	if walkErrorCount > 0 {
		// The vault was not fully readable (permissions, flaky mount, …).
		// Deleting everything absent from an incomplete listing would wipe
		// the index — abort instead and retry next tick.
		return stats, fmt.Errorf("vault walk had %d errors, skipping deletion reconciliation", walkErrorCount)
	}

	ix.syncFiles(ctx, log, files, known, force, &stats)

	if err := ix.backfillChunks(ctx); err != nil {
		log.Warn("backfill chunks", zap.Error(err))
		stats.Errors++
	}
	if ix.cfg.Features.Embeddings && ix.embedder != nil {
		if n, err := ix.backfillEmbeddings(ctx); err != nil {
			log.Warn("backfill chunks", zap.Error(err))
			stats.Errors++
		} else {
			stats.Embedded += n
		}
	}
	if len(files) == 0 && len(known) > 0 {
		// An entirely empty listing against a non-empty index almost certainly
		// means the volume is not mounted / not yet populated. Never purge.
		log.Error("vault listing is empty but index has notes; refusing to purge index",
			zap.Int("known", len(known)),
			zap.String("notes_dir", ix.cfg.NotesDir),
		)
		stats.Errors++
	}
	deletedRelpaths := make([]string, 0, len(known))
	for rel := range known {
		deletedRelpaths = append(deletedRelpaths, rel)
	}
	slices.Sort(deletedRelpaths)
	for _, rel := range deletedRelpaths {
		if err := DeleteNote(ctx, ix.pool, rel); err != nil {
			log.Warn("delete note", zap.String("relpath", rel), zap.Error(err))
			stats.Errors++
			continue
		}
		stats.Deleted++
	}
	if ix.cfg.Features.Profiles && ix.profiles != nil {
		profiled, itemErrors, err := ix.backfillProfiles(ctx)
		stats.Profiled += profiled
		stats.Errors += itemErrors
		if err != nil {
			log.Warn("backfill profiles", zap.Error(err))
			stats.Errors++
		}
	}
	if ix.cfg.Features.ProfileEmbeddings && ix.embedder != nil {
		if itemErrors, err := ix.backfillProfileEmbeddings(ctx); err != nil {
			log.Warn("backfill profile embeddings", zap.Error(err))
			stats.Errors++
		} else {
			stats.Errors += itemErrors
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
		zap.Int("profiled", stats.Profiled),
		zap.Int("errors", stats.Errors),
		zap.Duration("took", time.Since(start)),
	)
	return stats, nil
}

func (ix *Indexer) syncFiles(ctx context.Context, log *zap.Logger, files []vaultFile, known map[string]*NoteRow, force bool, stats *SyncStats) {
	ctx, span := telemetry.StartSpan(ctx, attribute.Int("search.sync.files.total", len(files)))
	defer span.End()

	for _, file := range files {
		existing := known[file.relpath]
		delete(known, file.relpath)
		if err := ix.syncFile(ctx, file.path, file.relpath, existing, force, stats); err != nil {
			log.Warn("sync file", zap.String("relpath", file.relpath), zap.Error(err))
			stats.Errors++
		}
	}
}

// discoverFiles walks the vault and returns markdown files plus the number of
// walk errors encountered (unreadable directories/files). Callers must not
// treat the result as a complete listing when walkErrors > 0.
func (ix *Indexer) discoverFiles(ctx context.Context, log *zap.Logger, stats *SyncStats) (files []vaultFile, walkErrors int, retErr error) {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	err := filepath.WalkDir(ix.cfg.NotesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warn("walk error", zap.String("path", path), zap.Error(err))
			stats.Errors++
			walkErrors++
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
		files = append(files, vaultFile{path: path, relpath: filepath.ToSlash(rel)})
		stats.Seen++
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walk vault: %w", err)
	}
	return files, walkErrors, nil
}

func (ix *Indexer) syncFile(ctx context.Context, fullPath, relpath string, existing *NoteRow, force bool, stats *SyncStats) error {
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	// Compare at microsecond precision: PostgreSQL TIMESTAMPTZ keeps
	// microseconds, while filesystem mtimes carry nanoseconds. Second-level
	// comparison used to skip files modified twice within one second with an
	// unchanged size.
	if !force && existing != nil &&
		existing.Mtime.Equal(info.ModTime().Truncate(time.Microsecond)) &&
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

	if err := ix.reindexChunks(ctx, noteID, doc); err != nil {
		return fmt.Errorf("reindex chunks: %w", err)
	}
	if ix.cfg.Features.Embeddings && ix.embedder != nil {
		source := "sync"
		if force {
			source = "force"
		}
		embedded, err := ix.reindexEmbeddings(ctx, noteID, name, doc.Metadata, force, source)
		if err != nil {
			return fmt.Errorf("reindex embeddings: %w", err)
		}
		stats.Embedded += embedded
	}
	return nil
}

// reindexChunks materializes chunk text and FTS independently of neural features.
func (ix *Indexer) reindexChunks(ctx context.Context, noteID int64, doc ParsedDocument) error {
	chunks := ChunkContent(doc.Body)
	existing, err := ListChunkHashes(ctx, ix.pool, noteID)
	if err != nil {
		return err
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

	embeddingsStale := len(staleIDs) > 0
	for _, c := range chunks {
		h := sha256Hash([]byte(string(c.Kind) + "\x00" + c.HeadingPath + "\x00" + c.Text))
		previous, exists := existingByKey[chunkKey(string(c.Kind), c.Ord)]
		if !exists || previous.Text != c.Text || previous.Heading != c.HeadingPath {
			embeddingsStale = true
		}
		if _, err := UpsertChunk(ctx, ix.pool, noteID, string(c.Kind), c.Ord, c.Text, c.HeadingPath, h); err != nil {
			return err
		}
	}
	if _, err := DeleteChunksByID(ctx, ix.pool, staleIDs); err != nil {
		return err
	}
	if embeddingsStale {
		if err := InvalidateNoteEmbeddings(ctx, ix.pool, noteID); err != nil {
			return err
		}
	}
	return MarkNoteChunksCurrent(ctx, ix.pool, noteID)
}

func (ix *Indexer) reindexEmbeddings(ctx context.Context, noteID int64, noteName string, metadata NoteMetadata, force bool, source string) (embedded int, err error) {
	started := time.Now()
	defer func() {
		if ix.metrics != nil {
			ix.metrics.recordIndexNote(ctx, source, embedded, time.Since(started), err)
		}
	}()
	rows, err := ListChunksForEmbedding(ctx, ix.pool, noteID)
	if err != nil {
		return 0, err
	}
	type pending struct {
		row   ChunkRow
		hash  []byte
		input string
	}
	var todo []pending
	for _, row := range rows {
		chunk := Chunk{Kind: ChunkKind(row.Kind), Ord: row.Ord, Text: row.Text, HeadingPath: row.Heading}
		input := embeddingInput(noteName, metadata, chunk)
		h := sha256Hash([]byte(input))
		if !force && row.EmbeddingVersion == CurrentEmbeddingVersion && row.EmbeddingModel == ix.cfg.EmbedModel && bytesEqual(row.EmbeddingHash, h) {
			continue
		}
		todo = append(todo, pending{row: row, hash: h, input: input})
	}
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
			if err := UpsertChunkEmbedding(ctx, ix.pool, p.row.ID, p.hash, vecs[i], ix.cfg.EmbedModel); err != nil {
				return embedded, err
			}
		}
		embedded += len(batch)
		start = end
	}
	if err := MarkNoteEmbeddingsCurrent(ctx, ix.pool, noteID, ix.cfg.EmbedModel); err != nil {
		return embedded, err
	}
	return embedded, nil
}

func chunkKey(kind string, ord int) string {
	return fmt.Sprintf("%s/%d", kind, ord)
}

// Chunk backfill is deliberately unbounded: it is local and cheap. The neural
// materializers have their own bounded queues.
const backfillPageSize = 200

func (ix *Indexer) backfillChunks(ctx context.Context) error {
	log := applog.With(ctx, logger)
	processed := 0
	var afterID int64
	stats := SyncStats{}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		page := backfillPageSize
		notes, err := NotesNeedingChunks(ctx, ix.pool, afterID, page)
		if err != nil {
			return err
		}
		if len(notes) == 0 {
			break
		}

		for _, n := range notes {
			afterID = n.ID
			doc := ParseDocument(n.Content, n.Name)
			if err := UpdateNoteParsed(ctx, ix.pool, n.ID, doc); err != nil {
				stats.Errors++
				log.Warn("backfill chunks: update parsed note", zap.Int64("note_id", n.ID), zap.String("relpath", n.Relpath), zap.Error(err))
				continue
			}
			if err := ix.reindexChunks(ctx, n.ID, doc); err != nil {
				stats.Errors++
				log.Warn("backfill chunks: reindex", zap.Int64("note_id", n.ID), zap.String("relpath", n.Relpath), zap.Error(err))
				continue
			}
			processed++
			if processed%100 == 0 {
				log.Info("backfill progress",
					zap.Int("notes_processed", processed),
				)
			}
		}
	}
	if processed > 0 {
		log.Info("backfill pass done",
			zap.Int("notes_processed", processed),
		)
	}
	return nil
}

func (ix *Indexer) backfillEmbeddings(ctx context.Context) (int, error) {
	log := applog.With(ctx, logger)
	limit, processed, embedded := ix.cfg.BackfillBatchPerPass, 0, 0
	var afterID int64
	for {
		if ctx.Err() != nil {
			return embedded, ctx.Err()
		}
		page := backfillPageSize
		if limit > 0 {
			if remaining := limit - processed; remaining <= 0 {
				break
			} else {
				page = min(page, remaining)
			}
		}
		notes, err := NotesNeedingEmbeddings(ctx, ix.pool, afterID, page, ix.cfg.EmbedModel)
		if err != nil {
			return embedded, err
		}
		if len(notes) == 0 {
			break
		}
		for _, note := range notes {
			afterID = note.ID
			doc := ParseDocument(note.Content, note.Name)
			n, err := ix.reindexEmbeddings(ctx, note.ID, note.Name, doc.Metadata, false, "backfill")
			if err != nil {
				if errors.Is(err, ErrEmbedderUnavailable) {
					return embedded, err
				}
				// Poison note: skip it so the rest of the queue is not starved.
				log.Warn("backfill embeddings: reindex",
					zap.Int64("note_id", note.ID),
					zap.String("relpath", note.Relpath),
					zap.Error(err),
				)
				continue
			}
			embedded += n
			processed++
		}
	}
	return embedded, nil
}

func (ix *Indexer) backfillProfiles(ctx context.Context) (profiled, itemErrors int, retErr error) {
	log := applog.With(ctx, logger)
	limit := ix.cfg.ProfileBackfillBatchPerPass
	processed := 0
	var afterID int64

	for {
		if ctx.Err() != nil {
			return profiled, itemErrors, ctx.Err()
		}
		page := backfillPageSize
		if limit > 0 {
			remaining := limit - processed
			if remaining <= 0 {
				break
			}
			page = min(page, remaining)
		}
		notes, err := NotesNeedingProfiles(ctx, ix.pool, afterID, page, ix.cfg.ProfileModel)
		if err != nil {
			return profiled, itemErrors, err
		}
		if len(notes) == 0 {
			break
		}
		for _, note := range notes {
			afterID = note.ID
			processed++
			doc := ParseDocument(note.Content, note.Name)
			note.Body = doc.Body
			note.Metadata = doc.Metadata
			profile, profileText, facets, err := ix.profiles.Extract(ctx, note)
			if err != nil {
				if errors.Is(err, ErrChatUnavailable) {
					return profiled, itemErrors, err
				}
				itemErrors++
				log.Warn("extract note profile", zap.Int64("note_id", note.ID), zap.String("relpath", note.Relpath), zap.Error(err))
				continue
			}
			if err := UpsertNoteProfile(ctx, ix.pool, note, profile, profileText, facets, ix.cfg.ProfileModel); err != nil {
				itemErrors++
				log.Warn("save note profile", zap.Int64("note_id", note.ID), zap.Error(err))
				continue
			}
			profiled++
		}
	}
	if processed > 0 {
		log.Info("profile backfill pass done",
			zap.Int("notes_processed", processed),
			zap.Int("profiles_indexed", profiled),
			zap.Int("errors", itemErrors),
		)
	}
	return profiled, itemErrors, nil
}

func (ix *Indexer) backfillProfileEmbeddings(ctx context.Context) (itemErrors int, retErr error) {
	limit, processed := ix.cfg.ProfileBackfillBatchPerPass, 0
	var afterID int64
	for {
		if ctx.Err() != nil {
			return itemErrors, ctx.Err()
		}
		page := backfillPageSize
		if limit > 0 {
			if remaining := limit - processed; remaining <= 0 {
				break
			} else {
				page = min(page, remaining)
			}
		}
		rows, err := ProfilesNeedingEmbeddings(ctx, ix.pool, afterID, page, ix.cfg.ProfileModel, ix.cfg.EmbedModel)
		if err != nil {
			return itemErrors, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			afterID = row.NoteID
			processed++
			vec, err := ix.embedder.EmbedOne(ctx, row.ProfileText, ix.metrics)
			if err != nil {
				itemErrors++
				continue
			}
			if err := UpsertProfileEmbedding(ctx, ix.pool, row.NoteID, row.SourceHash, vec, ix.cfg.ProfileModel, ix.cfg.EmbedModel); err != nil {
				itemErrors++
			}
		}
	}
	return itemErrors, nil
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
