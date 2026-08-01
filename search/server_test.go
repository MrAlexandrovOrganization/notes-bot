package search

import (
	"testing"

	pb "notes-bot/proto/search"
)

func TestSearchFiltersFromRequest(t *testing.T) {
	filters, err := searchFiltersFromRequest(&pb.SearchRequest{
		DateFrom: "31-Jul-2026",
		DateTo:   "2026-08-01",
		Kinds:    []string{"paragraph", "task"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filters.DateFrom == nil || filters.DateFrom.Format("2006-01-02") != "2026-07-31" {
		t.Fatalf("unexpected date_from: %v", filters.DateFrom)
	}
	if filters.DateTo == nil || filters.DateTo.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("unexpected date_to: %v", filters.DateTo)
	}
}

func TestSearchFiltersFromRequestRejectsInvalidValues(t *testing.T) {
	tests := []*pb.SearchRequest{
		{DateFrom: "tomorrow"},
		{DateFrom: "2026-08-02", DateTo: "2026-08-01"},
		{Kinds: []string{"note"}},
	}
	for _, req := range tests {
		if _, err := searchFiltersFromRequest(req); err == nil {
			t.Fatalf("expected validation error for %#v", req)
		}
	}
}

func TestHitsToProtoCarriesStructuredContext(t *testing.T) {
	got := hitsToProto([]SearchHit{{
		NoteID: 1, ChunkID: 2, NoteDate: "2026-07-31", Title: "Day",
		Tags: []string{"one"}, Links: []string{"ref"}, Heading: "Work", Ord: 3,
	}}, "")
	if len(got) != 1 || got[0].ChunkId != 2 || got[0].NoteDate != "2026-07-31" ||
		got[0].Title != "Day" || got[0].HeadingPath != "Work" || got[0].Ord != 3 {
		t.Fatalf("structured hit fields lost: %#v", got)
	}
}
