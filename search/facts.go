package search

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// noteFactsSchemaSQL defines the structured-facts layer (L2 in the layered
// model). Facts are typed, per-note signals extracted from note text over a
// curated vocabulary: place / person / event. Unlike free-text profiles, these
// rows are exact and queryable (e.g. COUNT days with place=gym, or list notes
// mentioning person=Alice), and they JOIN naturally with note metadata such as
// the daily rating stored on the notes row.
//
// The value set is a curated vocabulary (bounded, maintained as data rather
// than code); the planned LLM classifier resolves mentions to known values for
// determinism. place/person are naturally bounded; event should also become a
// closed activity vocabulary to keep aggregation exact.
const noteFactsSchemaSQL = `
CREATE TABLE IF NOT EXISTS note_facts (
    note_id     bigint        NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    kind        text          NOT NULL,
    value       text          NOT NULL,
    confidence  real          NOT NULL DEFAULT 1.0,
    extracted_at timestamptz  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (note_id, kind, value)
);
CREATE INDEX IF NOT EXISTS note_facts_kind_value ON note_facts (kind, value);
CREATE INDEX IF NOT EXISTS note_facts_note_id ON note_facts (note_id);
`

// FactKind enumerates the closed set of structured-fact types.
type FactKind string

const (
	FactPlace  FactKind = "place"
	FactPerson FactKind = "person"
	FactEvent  FactKind = "event"
)

// ErrFactsExtractionNotImplemented is returned by ExtractFacts until the LLM
// classifier is built. The layer, schema and materializer contract already exist
// so retrieval/agent can be wired against note_facts without the extractor.
var ErrFactsExtractionNotImplemented = errors.New("facts extraction not implemented")

// ExtractFacts is the (deferred) extraction entry point for the Facts layer.
// When implemented it should map a note's parsed content to typed facts over
// the curated vocabulary and upsert (note_id, kind, value) rows into note_facts.
// It must be idempotent: re-extraction replaces the fact set for the note.
func ExtractFacts(ctx context.Context, pool *pgxpool.Pool, noteID int64, doc ParsedDocument, cfg *Config) error {
	return ErrFactsExtractionNotImplemented
}

// UpsertNoteFacts replaces the entire fact set for a note. Defined now so the
// extraction implementation and any backfill have a stable write path.
func UpsertNoteFacts(ctx context.Context, pool *pgxpool.Pool, noteID int64, facts []NoteFact) error {
	if len(facts) == 0 {
		return nil
	}
	_ = pool
	_ = noteID
	_ = facts
	return ErrFactsExtractionNotImplemented
}

// NoteFact is a single structured signal attached to a note.
type NoteFact struct {
	Kind       FactKind
	Value      string
	Confidence float32
}
