package search

import "testing"

func TestFuseByChunkIDDeduplicatesAndAppliesDiversity(t *testing.T) {
	dense := []SearchHit{
		{ChunkID: 10, NoteID: 1},
		{ChunkID: 11, NoteID: 1},
		{ChunkID: 12, NoteID: 1},
		{ChunkID: 20, NoteID: 2},
	}
	lexical := []SearchHit{
		{ChunkID: 10, NoteID: 1},
		{ChunkID: 20, NoteID: 2},
		{ChunkID: 30, NoteID: 3},
	}
	got := FuseByChunkID(dense, lexical, 4, 2)
	if len(got) != 4 {
		t.Fatalf("want 4 diverse chunks, got %#v", got)
	}
	seen := make(map[int64]bool)
	perNote := make(map[int64]int)
	for _, hit := range got {
		if seen[hit.ChunkID] {
			t.Fatalf("duplicate chunk %d", hit.ChunkID)
		}
		seen[hit.ChunkID] = true
		perNote[hit.NoteID]++
		if perNote[hit.NoteID] > 2 {
			t.Fatalf("note diversity limit exceeded: %#v", got)
		}
	}
	if got[0].ChunkID != 10 {
		t.Fatalf("chunk present in both rankings should win, got %#v", got)
	}
}

func TestFuseByNoteIDDeduplicatesProfiles(t *testing.T) {
	dense := []SearchHit{{NoteID: 1}, {NoteID: 2}}
	lexical := []SearchHit{{NoteID: 2}, {NoteID: 3}}
	got := FuseByNoteID(dense, lexical, 3)
	if len(got) != 3 || got[0].NoteID != 2 {
		t.Fatalf("profile RRF did not reward overlap: %#v", got)
	}
}
