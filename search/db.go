package search

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"notes-bot/internal/telemetry"
)

// ftsDict is the single source of truth for the Postgres text-search config
// used both to build the generated tsv column (schemaSQL, migrateTSVDictSQL)
// and to parse queries against it (SearchByContent). These two sides MUST
// stay in sync — a stored tsvector built with one dictionary never matches a
// tsquery parsed with another, and the mismatch fails silently (zero rows,
// no error) rather than raising an error.
const ftsDict = "russian"

// CurrentIndexVersion is bumped whenever chunk boundaries or embedding input
// semantics change. Notes indexed by an older version are picked up by the
// resumable backfill without requiring a destructive table migration.
const CurrentIndexVersion = 2

var schemaSQL = fmt.Sprintf(`
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS notes (
    id             BIGSERIAL PRIMARY KEY,
    relpath        TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    mtime          TIMESTAMPTZ NOT NULL,
    size           BIGINT NOT NULL,
    content_hash   BYTEA NOT NULL,
    content        TEXT NOT NULL,
	body           TEXT NOT NULL DEFAULT '',
	note_date      DATE,
	title          TEXT NOT NULL DEFAULT '',
	tags           TEXT[] NOT NULL DEFAULT '{}',
	links          TEXT[] NOT NULL DEFAULT '{}',
	frontmatter    JSONB NOT NULL DEFAULT '{}'::jsonb,
	chunk_index_version INT NOT NULL DEFAULT 0,
	chunk_embedding_model TEXT NOT NULL DEFAULT '',
	chunks_indexed_at TIMESTAMPTZ,
    tsv            tsvector GENERATED ALWAYS AS
                     (to_tsvector('%[1]s', coalesce(name, '') || ' ' || coalesce(content, '')))
                     STORED,
    indexed_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS notes_tsv         ON notes USING GIN (tsv);
CREATE INDEX IF NOT EXISTS notes_name_trgm   ON notes USING GIN (name gin_trgm_ops);
`, ftsDict)

const notesMigrationSQL = `
ALTER TABLE notes ADD COLUMN IF NOT EXISTS body TEXT NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN IF NOT EXISTS note_date DATE;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE notes ADD COLUMN IF NOT EXISTS links TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE notes ADD COLUMN IF NOT EXISTS frontmatter JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS chunk_index_version INT NOT NULL DEFAULT 0;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS chunk_embedding_model TEXT NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN IF NOT EXISTS chunks_indexed_at TIMESTAMPTZ;
CREATE OR REPLACE FUNCTION effective_note_date(stored_date DATE, note_name TEXT)
RETURNS DATE
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $$
	SELECT COALESCE(
		stored_date,
		CASE
			WHEN note_name ~* '^[0-9]{2}-(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)-[0-9]{4}$'
			THEN to_date(note_name, 'DD-Mon-YYYY')
		END
	)
$$;
CREATE INDEX IF NOT EXISTS notes_note_date ON notes (note_date);
CREATE INDEX IF NOT EXISTS notes_tags_gin ON notes USING GIN (tags);
`

// migrateTSVDictSQL upgrades the tsv generated column to ftsDict when it was
// built with a different dictionary. The DO block checks the actual
// generation expression via pg_catalog before acting, so it is safe to run
// on every startup — it's a no-op when already on ftsDict.
var migrateTSVDictSQL = fmt.Sprintf(`
DO $$
DECLARE
    expr text;
BEGIN
    SELECT pg_get_expr(d.adbin, d.adrelid)
      INTO expr
      FROM pg_attribute a
      JOIN pg_class     c ON c.oid = a.attrelid
      JOIN pg_attrdef   d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
     WHERE c.relname = 'notes' AND a.attname = 'tsv';

    IF expr IS NOT NULL AND expr NOT LIKE '%%%[1]s%%' THEN
        ALTER TABLE notes DROP COLUMN tsv;
        ALTER TABLE notes ADD COLUMN tsv tsvector GENERATED ALWAYS AS
            (to_tsvector('%[1]s', coalesce(name, '') || ' ' || coalesce(content, ''))) STORED;
        DROP INDEX IF EXISTS notes_tsv;
        CREATE INDEX notes_tsv ON notes USING GIN (tsv);
    END IF;
END $$;
`, ftsDict)

const vectorSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS note_chunks (
    id           BIGSERIAL PRIMARY KEY,
    note_id      BIGINT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    ord          INT  NOT NULL,
    text         TEXT NOT NULL,
	heading_path TEXT NOT NULL DEFAULT '',
    chunk_hash   BYTEA NOT NULL,
    embedding    vector(%d) NOT NULL,
	embedding_model TEXT NOT NULL DEFAULT '',
	index_version INT NOT NULL DEFAULT 0,
	tsv_ru tsvector GENERATED ALWAYS AS
	       (to_tsvector('russian', coalesce(heading_path, '') || ' ' || coalesce(text, ''))) STORED,
	tsv_simple tsvector GENERATED ALWAYS AS
	           (to_tsvector('simple', coalesce(heading_path, '') || ' ' || coalesce(text, ''))) STORED,
    UNIQUE (note_id, kind, ord)
);

CREATE INDEX IF NOT EXISTS note_chunks_hnsw ON note_chunks
    USING hnsw (embedding vector_cosine_ops);
`

const vectorMigrationSQL = `
ALTER TABLE note_chunks ADD COLUMN IF NOT EXISTS heading_path TEXT NOT NULL DEFAULT '';
ALTER TABLE note_chunks ADD COLUMN IF NOT EXISTS embedding_model TEXT NOT NULL DEFAULT '';
ALTER TABLE note_chunks ADD COLUMN IF NOT EXISTS index_version INT NOT NULL DEFAULT 0;
ALTER TABLE note_chunks ADD COLUMN IF NOT EXISTS tsv_ru tsvector GENERATED ALWAYS AS
    (to_tsvector('russian', coalesce(heading_path, '') || ' ' || coalesce(text, ''))) STORED;
ALTER TABLE note_chunks ADD COLUMN IF NOT EXISTS tsv_simple tsvector GENERATED ALWAYS AS
    (to_tsvector('simple', coalesce(heading_path, '') || ' ' || coalesce(text, ''))) STORED;
CREATE INDEX IF NOT EXISTS note_chunks_tsv_ru ON note_chunks USING GIN (tsv_ru);
CREATE INDEX IF NOT EXISTS note_chunks_tsv_simple ON note_chunks USING GIN (tsv_simple);
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
	Content             string
	Body                string
	Metadata            NoteMetadata
	ChunkIndexVersion   int
	ChunkEmbeddingModel string
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
	if _, err := pool.Exec(ctx, notesMigrationSQL); err != nil {
		return fmt.Errorf("migrate notes schema: %w", err)
	}
	if _, err := pool.Exec(ctx, migrateTSVDictSQL); err != nil {
		return fmt.Errorf("migrate tsv dictionary: %w", err)
	}
	if enableVector {
		if _, err := pool.Exec(ctx, fmt.Sprintf(vectorSchemaSQL, embedDim)); err != nil {
			return fmt.Errorf("ensure vector schema: %w", err)
		}
		if _, err := pool.Exec(ctx, vectorMigrationSQL); err != nil {
			return fmt.Errorf("migrate vector schema: %w", err)
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
		INSERT INTO notes (
			relpath, name, mtime, size, content_hash, content, body,
			note_date, title, tags, links, frontmatter,
			chunk_index_version, chunk_embedding_model, indexed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		        COALESCE($10, '{}'::text[]), COALESCE($11, '{}'::text[]),
		        $12::jsonb, 0, '', NOW())
		ON CONFLICT (relpath) DO UPDATE SET
			name = EXCLUDED.name,
			mtime = EXCLUDED.mtime,
			size = EXCLUDED.size,
			content_hash = EXCLUDED.content_hash,
			content = EXCLUDED.content,
			body = EXCLUDED.body,
			note_date = EXCLUDED.note_date,
			title = EXCLUDED.title,
			tags = EXCLUDED.tags,
			links = EXCLUDED.links,
			frontmatter = EXCLUDED.frontmatter,
			chunk_index_version = 0,
			chunk_embedding_model = '',
			chunks_indexed_at = NULL,
			indexed_at = NOW()
		RETURNING id, (xmax = 0)
	`, n.Relpath, n.Name, n.Mtime, n.Size, n.ContentHash, n.Content, n.Body,
		n.Metadata.Date, n.Metadata.Title, n.Metadata.Tags, n.Metadata.Links,
		string(n.Metadata.FrontmatterJSON)).Scan(&id, &inserted)
	if err != nil {
		return 0, false, fmt.Errorf("upsert note: %w", err)
	}
	return id, inserted, nil
}

// UpdateNoteParsed refreshes body/frontmatter fields for notes created before
// structured metadata was introduced. It intentionally leaves index state
// untouched; MarkNoteChunksCurrent commits that state only after all chunks
// were embedded successfully.
func UpdateNoteParsed(ctx context.Context, pool *pgxpool.Pool, noteID int64, doc ParsedDocument) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, `
		UPDATE notes SET
			body = $2,
			note_date = $3,
			title = $4,
			tags = COALESCE($5, '{}'::text[]),
			links = COALESCE($6, '{}'::text[]),
			frontmatter = $7::jsonb
		WHERE id = $1
	`, noteID, doc.Body, doc.Metadata.Date, doc.Metadata.Title,
		doc.Metadata.Tags, doc.Metadata.Links, string(doc.Metadata.FrontmatterJSON))
	if err != nil {
		return fmt.Errorf("update parsed note: %w", err)
	}
	return nil
}

func MarkNoteChunksCurrent(ctx context.Context, pool *pgxpool.Pool, noteID int64, model string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, `
		UPDATE notes SET
			chunk_index_version = $2,
			chunk_embedding_model = $3,
			chunks_indexed_at = NOW()
		WHERE id = $1
	`, noteID, CurrentIndexVersion, model)
	if err != nil {
		return fmt.Errorf("mark note chunks current: %w", err)
	}
	return nil
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
	ChunkID   int64
	Relpath   string
	Name      string
	Snippet   string
	Score     float64
	ChunkKind string
	Heading   string
	Ord       int
	Neighbor  bool
	NoteDate  string
	Title     string
	Tags      []string
	Links     []string
}

type SearchFilters struct {
	DateFrom *time.Time
	DateTo   *time.Time
	Kinds    []string
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

// SearchByContent runs chunk-level FTS without optional filters. It is kept as
// the simple API used by the interactive note finder.
func SearchByContent(ctx context.Context, pool *pgxpool.Pool, query string, limit int) ([]SearchHit, error) {
	return SearchChunksByContent(ctx, pool, query, limit, SearchFilters{})
}

// SearchChunksByContent combines Russian stemming with a simple dictionary for
// names, English words and identifiers. ts_headline returns a passage around
// the actual match instead of the beginning of the note.
func SearchChunksByContent(ctx context.Context, pool *pgxpool.Pool, query string, limit int, filters SearchFilters) ([]SearchHit, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if limit <= 0 {
		limit = 10
	}
	rows, err := pool.Query(ctx, `
		WITH q AS (
			SELECT websearch_to_tsquery('russian', $1) AS ru,
			       websearch_to_tsquery('simple', $1) AS simple
		)
		SELECT n.id, c.id, n.relpath, n.name,
		       regexp_replace(
		           CASE WHEN c.tsv_simple @@ q.simple
		                THEN ts_headline('simple', c.text, q.simple,
		                                 'MaxWords=60, MinWords=20, ShortWord=2')
		                ELSE ts_headline('russian', c.text, q.ru,
		                                 'MaxWords=60, MinWords=20, ShortWord=2')
		           END,
		           '<[^>]+>', '', 'g'
		       ) AS snippet,
		       ts_rank_cd(c.tsv_ru, q.ru) + 0.5 * ts_rank_cd(c.tsv_simple, q.simple) AS score,
		       c.kind, c.heading_path, c.ord,
		       COALESCE(to_char(effective_note_date(n.note_date, n.name), 'YYYY-MM-DD'), ''),
		       n.title, n.tags, n.links
		FROM note_chunks c
		JOIN notes n ON n.id = c.note_id
		CROSS JOIN q
		WHERE (c.tsv_ru @@ q.ru OR c.tsv_simple @@ q.simple)
		  AND c.kind <> 'note'
		  AND ($2::date IS NULL OR effective_note_date(n.note_date, n.name) >= $2::date)
		  AND ($3::date IS NULL OR effective_note_date(n.note_date, n.name) <= $3::date)
		  AND ($4::text[] IS NULL OR c.kind = ANY($4::text[]))
		ORDER BY score DESC, c.id
		LIMIT $5
	`, query, filters.DateFrom, filters.DateTo, nullableStrings(filters.Kinds), limit)
	if err != nil {
		return nil, fmt.Errorf("search by content: %w", err)
	}
	defer rows.Close()

	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.NoteID, &h.ChunkID, &h.Relpath, &h.Name, &h.Snippet,
			&h.Score, &h.ChunkKind, &h.Heading, &h.Ord,
			&h.NoteDate, &h.Title, &h.Tags, &h.Links); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
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

func CountNotesPendingIndex(ctx context.Context, pool *pgxpool.Pool, model string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM notes
		WHERE chunk_index_version <> $1 OR chunk_embedding_model <> $2
	`, CurrentIndexVersion, model).Scan(&n)
	return n, err
}

// IndexMetricsSnapshot is read in one SQL statement so all gauges exported in
// a Prometheus scrape describe the same database state.
type IndexMetricsSnapshot struct {
	TotalNotes        int64
	PendingNotes      int64
	TotalChunks       int64
	CurrentChunks     int64
	LatestIndexedUnix int64
}

func ReadIndexMetricsSnapshot(ctx context.Context, pool *pgxpool.Pool, model string) (IndexMetricsSnapshot, error) {
	var snapshot IndexMetricsSnapshot
	err := pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM notes),
			(SELECT COUNT(*) FROM notes
			 WHERE chunk_index_version <> $1 OR chunk_embedding_model <> $2),
			(SELECT COUNT(*) FROM note_chunks),
			(SELECT COUNT(*) FROM note_chunks
			 WHERE index_version = $1 AND embedding_model = $2),
			COALESCE((SELECT EXTRACT(EPOCH FROM MAX(chunks_indexed_at))::bigint
			          FROM notes
			          WHERE chunk_index_version = $1 AND chunk_embedding_model = $2), 0)
	`, CurrentIndexVersion, model).Scan(
		&snapshot.TotalNotes,
		&snapshot.PendingNotes,
		&snapshot.TotalChunks,
		&snapshot.CurrentChunks,
		&snapshot.LatestIndexedUnix,
	)
	if err != nil {
		return IndexMetricsSnapshot{}, fmt.Errorf("read index metrics snapshot: %w", err)
	}
	return snapshot, nil
}

// NotesNeedingChunks returns notes whose committed index version/model is stale.
// The note-level marker is updated only after a complete reindex, so interrupted
// deployments resume safely even if some chunk rows were already written.
func NotesNeedingChunks(ctx context.Context, pool *pgxpool.Pool, afterID int64, limit int, model string) ([]NoteFull, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if limit <= 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT n.id, n.relpath, n.name, n.mtime, n.size, n.content_hash, n.content,
		       n.chunk_index_version, n.chunk_embedding_model
		FROM notes n
		WHERE n.id > $1
		  AND (n.chunk_index_version <> $3 OR n.chunk_embedding_model <> $4)
		ORDER BY n.id ASC
		LIMIT $2
	`, afterID, limit, CurrentIndexVersion, model)
	if err != nil {
		return nil, fmt.Errorf("notes missing chunks: %w", err)
	}
	defer rows.Close()
	var out []NoteFull
	for rows.Next() {
		var n NoteFull
		if err := rows.Scan(&n.ID, &n.Relpath, &n.Name, &n.Mtime, &n.Size, &n.ContentHash, &n.Content,
			&n.ChunkIndexVersion, &n.ChunkEmbeddingModel); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ChunkRow is a row in note_chunks (without embedding or text — those are
// fetched only on demand to keep listings cheap).
type ChunkRow struct {
	ID        int64
	NoteID    int64
	Kind      string
	Ord       int
	ChunkHash []byte
}

// vecLiteral serialises a float32 vector to the pgvector textual format
// "[v1,v2,...]". pgvector accepts this on insert and uses fewer bytes than
// the binary protocol in pgx without the pgvector-pgx adapter.
func vecLiteral(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 8)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		// strconv would be faster but fmt is fine for our batch sizes (~hundreds).
		fmt.Fprintf(&b, "%g", x)
	}
	b.WriteByte(']')
	return b.String()
}

// ListChunkHashes returns existing (kind, ord, hash) triples for a note.
// Used by the indexer to decide which chunks need re-embedding.
func ListChunkHashes(ctx context.Context, pool *pgxpool.Pool, noteID int64) ([]ChunkRow, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	rows, err := pool.Query(ctx,
		`SELECT id, note_id, kind, ord, chunk_hash FROM note_chunks WHERE note_id = $1`,
		noteID)
	if err != nil {
		return nil, fmt.Errorf("list chunk hashes: %w", err)
	}
	defer rows.Close()
	var out []ChunkRow
	for rows.Next() {
		var c ChunkRow
		if err := rows.Scan(&c.ID, &c.NoteID, &c.Kind, &c.Ord, &c.ChunkHash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertChunk inserts or updates a chunk row. Embeddings are written only when
// content actually changes — callers should skip the embed call if the hash
// matches an existing row.
func UpsertChunk(ctx context.Context, pool *pgxpool.Pool, noteID int64, kind string, ord int, text, heading string, hash []byte, vec []float32, model string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	_, err := pool.Exec(ctx, `
		INSERT INTO note_chunks (
			note_id, kind, ord, text, heading_path, chunk_hash, embedding,
			embedding_model, index_version
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::vector, $8, $9)
		ON CONFLICT (note_id, kind, ord) DO UPDATE SET
			text       = EXCLUDED.text,
			heading_path = EXCLUDED.heading_path,
			chunk_hash = EXCLUDED.chunk_hash,
			embedding  = EXCLUDED.embedding,
			embedding_model = EXCLUDED.embedding_model,
			index_version = EXCLUDED.index_version
	`, noteID, kind, ord, text, heading, hash, vecLiteral(vec), model, CurrentIndexVersion)
	if err != nil {
		return fmt.Errorf("upsert chunk: %w", err)
	}
	return nil
}

func DeleteChunksByID(ctx context.Context, pool *pgxpool.Pool, ids []int64) (int64, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := pool.Exec(ctx, `DELETE FROM note_chunks WHERE id = ANY($1::bigint[])`, ids)
	if err != nil {
		return 0, fmt.Errorf("delete stale chunks: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SearchByVector runs cosine search over filtered chunks. The materialized
// candidate CTE deliberately uses an exact scan: this vault is small, and exact
// ordering avoids HNSW under-returning after selective date/kind filters.
func SearchByVector(ctx context.Context, pool *pgxpool.Pool, vec []float32, limit int, filters SearchFilters) ([]SearchHit, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if limit <= 0 {
		limit = 8
	}

	query := `
		WITH candidates AS MATERIALIZED (
			SELECT n.id AS note_id, c.id AS chunk_id, n.relpath, n.name,
			       c.text, c.embedding, c.kind, c.heading_path, c.ord,
			       COALESCE(to_char(effective_note_date(n.note_date, n.name), 'YYYY-MM-DD'), '') AS note_date,
			       n.title, n.tags, n.links
			FROM note_chunks c
			JOIN notes n ON n.id = c.note_id
			WHERE c.kind <> 'note'
			  AND ($2::date IS NULL OR effective_note_date(n.note_date, n.name) >= $2::date)
			  AND ($3::date IS NULL OR effective_note_date(n.note_date, n.name) <= $3::date)
			  AND ($4::text[] IS NULL OR c.kind = ANY($4::text[]))
		)
		SELECT note_id, chunk_id, relpath, name, text,
		       1 - (embedding <=> $1::vector) AS score,
		       kind, heading_path, ord, note_date, title, tags, links
		FROM candidates
		ORDER BY embedding <=> $1::vector ASC, chunk_id
		LIMIT $5
	`
	rows, err := pool.Query(ctx, query, vecLiteral(vec), filters.DateFrom, filters.DateTo,
		nullableStrings(filters.Kinds), limit)
	if err != nil {
		return nil, fmt.Errorf("ann search: %w", err)
	}
	defer rows.Close()

	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.NoteID, &h.ChunkID, &h.Relpath, &h.Name, &h.Snippet,
			&h.Score, &h.ChunkKind, &h.Heading, &h.Ord,
			&h.NoteDate, &h.Title, &h.Tags, &h.Links); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func nullableStrings(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}
