package search

import (
	"strings"
	"testing"
)

// TestFTSDictConsistency guards against the class of bug where the tsvector
// generated column (schemaSQL/migrateTSVDictSQL) is built with one Postgres
// text-search dictionary while SearchByContent parses queries with another.
// That mismatch doesn't error — it just silently returns zero rows, which is
// how the "search doesn't find anything" regression shipped previously.
func TestFTSDictConsistency(t *testing.T) {
	if ftsDict == "" {
		t.Fatal("ftsDict must not be empty")
	}
	want := "to_tsvector('" + ftsDict + "'"
	if !strings.Contains(schemaSQL, want) {
		t.Errorf("schemaSQL does not build the generated tsv column with ftsDict %q; want substring %q", ftsDict, want)
	}
	if !strings.Contains(migrateTSVDictSQL, want) {
		t.Errorf("migrateTSVDictSQL does not migrate the tsv column to ftsDict %q; want substring %q", ftsDict, want)
	}
}

func TestVersionedChunkSchemaContainsRollingMigrationColumns(t *testing.T) {
	if CurrentIndexVersion <= 0 {
		t.Fatal("CurrentIndexVersion must be positive")
	}
	for _, want := range []string{
		"chunk_index_version", "chunk_embedding_model", "note_date",
		"frontmatter JSONB", "heading_path", "embedding_model",
		"index_version", "tsv_ru", "tsv_simple",
	} {
		if !strings.Contains(notesMigrationSQL+vectorMigrationSQL, want) {
			t.Errorf("rolling migrations do not contain %q", want)
		}
	}
}
