package search

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FuseByChunkID combines dense and lexical rankings, then applies note-level
// diversity. Deduplication before diversity is essential: repeated
// representations must not consume the retrieval budget.
func FuseByChunkID(dense, lexical []SearchHit, limit, maxPerNote int) []SearchHit {
	if limit <= 0 {
		limit = 12
	}
	if maxPerNote <= 0 {
		maxPerNote = 2
	}

	const rrfK = 60.0
	type candidate struct {
		hit   SearchHit
		score float64
	}
	candidates := make(map[int64]candidate, len(dense)+len(lexical))
	add := func(hits []SearchHit) {
		for rank, hit := range hits {
			if hit.ChunkID == 0 {
				continue
			}
			candidate, exists := candidates[hit.ChunkID]
			if !exists {
				candidate.hit = hit
			}
			candidate.score += 1 / (rrfK + float64(rank+1))
			candidates[hit.ChunkID] = candidate
		}
	}
	add(dense)
	add(lexical)

	ranked := make([]candidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.hit.Score = candidate.score
		ranked = append(ranked, candidate)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].hit.ChunkID < ranked[j].hit.ChunkID
		}
		return ranked[i].score > ranked[j].score
	})

	perNote := make(map[int64]int)
	out := make([]SearchHit, 0, min(limit, len(ranked)))
	for _, candidate := range ranked {
		if perNote[candidate.hit.NoteID] >= maxPerNote {
			continue
		}
		perNote[candidate.hit.NoteID]++
		out = append(out, candidate.hit)
		if len(out) == limit {
			break
		}
	}
	return out
}

// FuseByNoteID is the profile-index counterpart of FuseByChunkID. Profile
// rows have no source chunk id because they are routing documents, not evidence.
func FuseByNoteID(dense, lexical []SearchHit, limit int) []SearchHit {
	return FuseManyByNoteID(limit, dense, lexical)
}

// FuseManyByNoteID combines an arbitrary product-selected set of retrievers.
func FuseManyByNoteID(limit int, rankings ...[]SearchHit) []SearchHit {
	if limit <= 0 {
		limit = 20
	}
	const rrfK = 60.0
	type candidate struct {
		hit   SearchHit
		score float64
	}
	candidates := make(map[int64]candidate)
	add := func(hits []SearchHit) {
		for rank, hit := range hits {
			if hit.NoteID == 0 {
				continue
			}
			c := candidates[hit.NoteID]
			if c.hit.NoteID == 0 {
				c.hit = hit
			}
			c.score += 1 / (rrfK + float64(rank+1))
			candidates[hit.NoteID] = c
		}
	}
	for _, ranking := range rankings {
		add(ranking)
	}
	ranked := make([]candidate, 0, len(candidates))
	for _, c := range candidates {
		c.hit.Score = c.score
		ranked = append(ranked, c)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].hit.NoteID < ranked[j].hit.NoteID
		}
		return ranked[i].score > ranked[j].score
	})
	out := make([]SearchHit, 0, min(limit, len(ranked)))
	for _, c := range ranked {
		out = append(out, c.hit)
		if len(out) == limit {
			break
		}
	}
	return out
}

// ExpandChunkNeighbors returns exact source text for each selected chunk and
// its adjacent chunks. Results are emitted in source order per retrieval hit;
// overlapping windows are globally deduplicated by chunk id.
func ExpandChunkNeighbors(ctx context.Context, pool *pgxpool.Pool, selected []SearchHit, radius int) ([]SearchHit, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	if radius < 0 {
		radius = 0
	}

	noteIDs := make([]int64, len(selected))
	ords := make([]int32, len(selected))
	chunkIDs := make([]int64, len(selected))
	for i, hit := range selected {
		noteIDs[i] = hit.NoteID
		ords[i] = int32(hit.Ord)
		chunkIDs[i] = hit.ChunkID
	}

	rows, err := pool.Query(ctx, `
		WITH targets AS (
			SELECT *
			FROM unnest($1::bigint[], $2::int[], $3::bigint[])
			     WITH ORDINALITY AS t(note_id, target_ord, target_chunk_id, pos)
		)
		SELECT t.pos, t.target_chunk_id,
		       n.id, c.id, n.relpath, n.name, c.text, c.kind, c.heading_path, c.ord,
		       COALESCE(to_char(effective_note_date(n.note_date, n.name), 'YYYY-MM-DD'), ''),
		       n.title, n.tags, n.links
		FROM targets t
		JOIN notes n ON n.id = t.note_id
		JOIN note_chunks c ON c.note_id = t.note_id
		  AND c.kind <> 'note'
		  AND c.ord BETWEEN t.target_ord - $4 AND t.target_ord + $4
		ORDER BY t.pos, c.ord, c.id
	`, noteIDs, ords, chunkIDs, radius)
	if err != nil {
		return nil, fmt.Errorf("expand chunk neighbors: %w", err)
	}
	defer rows.Close()

	capacity := len(selected) * (radius*2 + 1)
	seen := make(map[int64]struct{}, capacity)
	out := make([]SearchHit, 0, capacity)
	for rows.Next() {
		var pos int
		var targetChunkID int64
		var hit SearchHit
		if err := rows.Scan(&pos, &targetChunkID, &hit.NoteID, &hit.ChunkID,
			&hit.Relpath, &hit.Name, &hit.Snippet, &hit.ChunkKind, &hit.Heading, &hit.Ord,
			&hit.NoteDate, &hit.Title, &hit.Tags, &hit.Links); err != nil {
			return nil, err
		}
		if _, exists := seen[hit.ChunkID]; exists {
			continue
		}
		seen[hit.ChunkID] = struct{}{}
		hit.Neighbor = hit.ChunkID != targetChunkID
		if pos > 0 && pos <= len(selected) {
			hit.Score = selected[pos-1].Score
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}
