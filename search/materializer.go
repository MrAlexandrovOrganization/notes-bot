package search

import (
	"context"
)

// Materializer turns already-ingested notes into one derived search artifact
// (chunks, embeddings, profiles, facts, ...). Each layer owns its ingestion
// policy and is independently enabled/configured, so the indexer no longer
// carries a scattered tree of `if Features.X` branches.
//
// A Materializer is intentionally coarse: it either works eagerly on a single
// freshly-upserted note (SyncNote) or resumes any backlog for its artifact
// (Backfill). Layers are wired by BuildMaterializers from the process config,
// which keeps the "what to build" decision in one place instead of five.
type Materializer interface {
	// Name identifies the layer in logs, metrics and schema versions.
	Name() string
	// Enabled reports whether this layer is active for the current config.
	Enabled() bool
	// SyncNote materializes the artifact for a single freshly-upserted note.
	SyncNote(ctx context.Context, noteID int64, name string, doc ParsedDocument, force bool, stats *SyncStats) error
	// Backfill resumes materialization for any notes still missing the artifact.
	Backfill(ctx context.Context, stats *SyncStats) error
}

// forceResetter is an optional capability for layers whose artifact must be
// invalidated wholesale before a ForceReindex pass (e.g. LLM-derived profiles).
type forceResetter interface {
	InvalidateAll(ctx context.Context) error
}

// ChunkMaterializer materializes chunk text + FTS. It is the lexical baseline
// and has no neural dependency, so it is always enabled.
type ChunkMaterializer struct {
	ix *Indexer
}

func (m *ChunkMaterializer) Name() string  { return "chunks" }
func (m *ChunkMaterializer) Enabled() bool { return true }

func (m *ChunkMaterializer) SyncNote(ctx context.Context, noteID int64, _ string, doc ParsedDocument, _ bool, _ *SyncStats) error {
	return m.ix.reindexChunks(ctx, noteID, doc)
}

func (m *ChunkMaterializer) Backfill(ctx context.Context, stats *SyncStats) error {
	if err := m.ix.backfillChunks(ctx); err != nil {
		stats.Errors++
		return nil
	}
	return nil
}

// EmbeddingMaterializer materializes dense vectors for chunks. It exists only
// when FEATURE_EMBEDDINGS is on and an embedder is reachable.
type EmbeddingMaterializer struct {
	ix *Indexer
}

func (m *EmbeddingMaterializer) Name() string { return "embeddings" }
func (m *EmbeddingMaterializer) Enabled() bool {
	return m.ix.cfg.Features.Embeddings && m.ix.embedder != nil
}

func (m *EmbeddingMaterializer) SyncNote(ctx context.Context, noteID int64, name string, doc ParsedDocument, force bool, stats *SyncStats) error {
	source := "sync"
	if force {
		source = "force"
	}
	embedded, err := m.ix.reindexEmbeddings(ctx, noteID, name, doc.Metadata, force, source)
	if err != nil {
		return err
	}
	stats.Embedded += embedded
	return nil
}

func (m *EmbeddingMaterializer) Backfill(ctx context.Context, stats *SyncStats) error {
	n, err := m.ix.backfillEmbeddings(ctx)
	if err != nil {
		stats.Errors++
		return nil
	}
	stats.Embedded += n
	return nil
}

// ProfileMaterializer materializes LLM note summaries used as navigation
// routing. Disabled by default (FEATURE_PROFILES); extraction is lazy/backfill
// only, so SyncNote is a no-op.
type ProfileMaterializer struct {
	ix *Indexer
}

func (m *ProfileMaterializer) Name() string { return "profiles" }
func (m *ProfileMaterializer) Enabled() bool {
	return m.ix.cfg.Features.Profiles && m.ix.profiles != nil
}

func (m *ProfileMaterializer) SyncNote(context.Context, int64, string, ParsedDocument, bool, *SyncStats) error {
	return nil
}

func (m *ProfileMaterializer) Backfill(ctx context.Context, stats *SyncStats) error {
	profiled, itemErrors, err := m.ix.backfillProfiles(ctx)
	stats.Profiled += profiled
	stats.Errors += itemErrors
	if err != nil {
		stats.Errors++
		return nil
	}
	return nil
}

func (m *ProfileMaterializer) InvalidateAll(ctx context.Context) error {
	return InvalidateNoteProfiles(ctx, m.ix.pool)
}

// ProfileEmbeddingMaterializer materializes dense vectors for note profiles.
type ProfileEmbeddingMaterializer struct {
	ix *Indexer
}

func (m *ProfileEmbeddingMaterializer) Name() string { return "profile_embeddings" }
func (m *ProfileEmbeddingMaterializer) Enabled() bool {
	return m.ix.cfg.Features.ProfileEmbeddings && m.ix.embedder != nil
}

func (m *ProfileEmbeddingMaterializer) SyncNote(context.Context, int64, string, ParsedDocument, bool, *SyncStats) error {
	return nil
}

func (m *ProfileEmbeddingMaterializer) Backfill(ctx context.Context, stats *SyncStats) error {
	itemErrors, err := m.ix.backfillProfileEmbeddings(ctx)
	if err != nil {
		stats.Errors++
		return nil
	}
	stats.Errors += itemErrors
	return nil
}

// FactsMaterializer is the structured-facts layer (place/person/event over a
// curated vocabulary). The schema is wired and the layer is addressable, but
// the LLM classifier that fills note_facts is intentionally not implemented
// yet — see FactsMaterializer.Backfill / ExtractFacts in facts.go.
type FactsMaterializer struct {
	ix *Indexer
}

func (m *FactsMaterializer) Name() string  { return "facts" }
func (m *FactsMaterializer) Enabled() bool { return m.ix.cfg.Features.Facts }

func (m *FactsMaterializer) SyncNote(context.Context, int64, string, ParsedDocument, bool, *SyncStats) error {
	return nil
}

func (m *FactsMaterializer) Backfill(ctx context.Context, stats *SyncStats) error {
	// Extraction deferred: when implemented, iterate notes missing facts and
	// upsert (note_id, kind, value) rows via ExtractFacts. No-op until then.
	return nil
}

// buildMaterializers assembles the active layer set from config. The order in
// the slice is the backfill order and is preserved across sync passes.
func (ix *Indexer) buildMaterializers() []Materializer {
	ms := []Materializer{&ChunkMaterializer{ix: ix}}
	if ix.cfg.Features.Embeddings && ix.embedder != nil {
		ms = append(ms, &EmbeddingMaterializer{ix: ix})
	}
	if ix.cfg.Features.Profiles && ix.profiles != nil {
		ms = append(ms, &ProfileMaterializer{ix: ix})
	}
	if ix.cfg.Features.ProfileEmbeddings && ix.embedder != nil {
		ms = append(ms, &ProfileEmbeddingMaterializer{ix: ix})
	}
	if ix.cfg.Features.Facts {
		ms = append(ms, &FactsMaterializer{ix: ix})
	}
	return ms
}
