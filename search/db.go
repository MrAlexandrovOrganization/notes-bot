package search

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"notes-bot/internal/telemetry"
)

const schemaSQL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS notes (
    id             BIGSERIAL PRIMARY KEY,
    relpath        TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    mtime          TIMESTAMPTZ NOT NULL,
    size           BIGINT NOT NULL,
    content_hash   BYTEA NOT NULL,
    content        TEXT NOT NULL,
    tsv            tsvector GENERATED ALWAYS AS
                     (to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(content, '')))
                     STORED,
    indexed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notes_tsv         ON notes USING GIN (tsv);
CREATE INDEX IF NOT EXISTS notes_name_trgm   ON notes USING GIN (name gin_trgm_ops);
`

const vectorSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS note_chunks (
    id           BIGSERIAL PRIMARY KEY,
    note_id      BIGINT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    ord          INT  NOT NULL,
    text         TEXT NOT NULL,
    chunk_hash   BYTEA NOT NULL,
    embedding    vector(%d) NOT NULL,
    UNIQUE (note_id, kind, ord)
);

CREATE INDEX IF NOT EXISTS note_chunks_hnsw ON note_chunks
    USING hnsw (embedding vector_cosine_ops);
`

// NoteRow mirrors a row in the notes table (without content/tsv for list operations).
type NoteRow struct {
	ID          int64
	Relpath     string
	Name        string
	Mtime       time.Time
	Size        int64
	ContentHash []byte
}

type NoteFull struct {
	NoteRow
	Content string
}

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	config.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// EnsureSchema creates the notes table and indexes. When enableVector is true,
// also installs the pgvector extension and note_chunks table sized to embedDim.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool, enableVector bool, embedDim int) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ensure notes schema: %w", err)
	}
	if enableVector {
		if _, err := pool.Exec(ctx, fmt.Sprintf(vectorSchemaSQL, embedDim)); err != nil {
			return fmt.Errorf("ensure vector schema: %w", err)
		}
	}
	logger.Info("database schema ensured")
	return nil
}

// UpsertNote inserts or updates a note row. Returns the resulting note id and
// a flag indicating whether the row was newly created (true) or updated (false).
func UpsertNote(ctx context.Context, pool *pgxpool.Pool, n NoteFull) (int64, bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	var id int64
	var inserted bool
	err := pool.QueryRow(ctx, `
		INSERT INTO notes (relpath, name, mtime, size, content_hash, content, indexed_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (relpath) DO UPDATE SET
			name = EXCLUDED.name,
			mtime = EXCLUDED.mtime,
			size = EXCLUDED.size,
			content_hash = EXCLUDED.content_hash,
			content = EXCLUDED.content,
			indexed_at = NOW()
		RETURNING id, (xmax = 0)
	`, n.Relpath, n.Name, n.Mtime, n.Size, n.ContentHash, n.Content).Scan(&id, &inserted)
	if err != nil {
		return 0, false, fmt.Errorf("upsert note: %w", err)
	}
	return id, inserted, nil
}

// TouchNoteMeta updates only mtime/size for a note whose content hash matched.
// Avoids rewriting the (potentially large) content column.
func TouchNoteMeta(ctx context.Context, pool *pgxpool.Pool, relpath string, mtime time.Time, size int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx,
		`UPDATE notes SET mtime = $1, size = $2, indexed_at = NOW() WHERE relpath = $3`,
		mtime, size, relpath)
	if err != nil {
		return fmt.Errorf("touch note: %w", err)
	}
	return nil
}

func GetNoteMeta(ctx context.Context, pool *pgxpool.Pool, relpath string) (*NoteRow, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	row := pool.QueryRow(ctx,
		`SELECT id, relpath, name, mtime, size, content_hash FROM notes WHERE relpath = $1`,
		relpath)
	var n NoteRow
	err := row.Scan(&n.ID, &n.Relpath, &n.Name, &n.Mtime, &n.Size, &n.ContentHash)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note meta: %w", err)
	}
	return &n, nil
}

func GetNoteByID(ctx context.Context, pool *pgxpool.Pool, id int64) (*NoteFull, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	row := pool.QueryRow(ctx,
		`SELECT id, relpath, name, mtime, size, content_hash, content FROM notes WHERE id = $1`,
		id)
	var n NoteFull
	err := row.Scan(&n.ID, &n.Relpath, &n.Name, &n.Mtime, &n.Size, &n.ContentHash, &n.Content)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by id: %w", err)
	}
	return &n, nil
}

func GetNoteByRelpath(ctx context.Context, pool *pgxpool.Pool, relpath string) (*NoteFull, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	row := pool.QueryRow(ctx,
		`SELECT id, relpath, name, mtime, size, content_hash, content FROM notes WHERE relpath = $1`,
		relpath)
	var n NoteFull
	err := row.Scan(&n.ID, &n.Relpath, &n.Name, &n.Mtime, &n.Size, &n.ContentHash, &n.Content)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by relpath: %w", err)
	}
	return &n, nil
}

func DeleteNote(ctx context.Context, pool *pgxpool.Pool, relpath string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, `DELETE FROM notes WHERE relpath = $1`, relpath)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return nil
}

func AllRelpaths(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	rows, err := pool.Query(ctx, `SELECT relpath FROM notes`)
	if err != nil {
		return nil, fmt.Errorf("list relpaths: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SearchHit is the DB-level search result, projected from a notes row.
type SearchHit struct {
	NoteID    int64
	Relpath   string
	Name      string
	Snippet   string
	Score     float64
	ChunkKind string
}

// SearchByName returns notes whose name matches the query via pg_trgm similarity.
func SearchByName(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]SearchHit, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if limit <= 0 {
		limit = 10
	}
	rows, err := pool.Query(ctx, `
		SELECT id, relpath, name,
		       LEFT(content, 200) AS snippet,
		       similarity(name, $1) AS score
		FROM notes
		WHERE name ILIKE '%' || $1 || '%' OR name % $1
		ORDER BY score DESC, name ASC
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search by name: %w", err)
	}
	defer rows.Close()
	return scanHits(rows)
}

// SearchByContent runs a websearch FTS query over name+content.
func SearchByContent(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]SearchHit, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if limit <= 0 {
		limit = 10
	}
	rows, err := pool.Query(ctx, `
		SELECT id, relpath, name,
		       LEFT(content, 200) AS snippet,
		       ts_rank(tsv, websearch_to_tsquery('simple', $1)) AS score
		FROM notes
		WHERE tsv @@ websearch_to_tsquery('simple', $1)
		ORDER BY score DESC
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search by content: %w", err)
	}
	defer rows.Close()
	return scanHits(rows)
}

func scanHits(rows pgx.Rows) ([]SearchHit, error) {
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.NoteID, &h.Relpath, &h.Name, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func CountNotes(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notes`).Scan(&n)
	return n, err
}
