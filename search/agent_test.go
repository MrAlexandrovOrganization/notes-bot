package search

import (
	"strings"
	"testing"
	"time"
)

func TestRenderLedgerReservesBudgetForRawEvidence(t *testing.T) {
	ledger := newAgentLedger()
	ledger.scans = append(ledger.scans, agentScanSummary{Query: "зал", Chunks: 120, DistinctDates: 80})
	for i := int64(1); i <= 10; i++ {
		ledger.addProfiles([]SearchHit{{NoteID: i, Name: "profile", Snippet: strings.Repeat("p", 200)}})
	}
	ledger.addEvidence([]SearchHit{{NoteID: 1, ChunkID: 99, Name: "01-Aug-2026", Snippet: "RAW-EVIDENCE"}})
	got := renderLedger(ledger, 1200)
	if !strings.Contains(got, "RAW-EVIDENCE") {
		t.Fatalf("raw evidence was crowded out by profiles: %q", got)
	}
	if !strings.Contains(got, `EXHAUSTIVE FTS "зал"`) {
		t.Fatalf("exhaustive coverage summary missing: %q", got)
	}
}

func TestLedgerAllowsHybridAndExhaustiveForSameText(t *testing.T) {
	ledger := newAgentLedger()
	if !ledger.addSearch("hybrid", "зал") || !ledger.addSearch("exhaustive", "зал") {
		t.Fatal("different retrieval modes were incorrectly deduplicated")
	}
	if ledger.addSearch("hybrid", " ЗАЛ ") {
		t.Fatal("same mode/query was not deduplicated")
	}
}

func TestSelectedNotesFallsBackToRawEvidenceWithoutProfiles(t *testing.T) {
	ledger := newAgentLedger()
	ledger.addEvidence([]SearchHit{{NoteID: 7, ChunkID: 70}, {NoteID: 8, ChunkID: 80}})
	got := ledger.selectedNoteIDs(10)
	if len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("selectedNoteIDs() = %#v", got)
	}
}

func TestMergeAgentFiltersCannotBroadenExplicitRange(t *testing.T) {
	from := mustAgentDate(t, "2026-07-01")
	to := mustAgentDate(t, "2026-07-31")
	got := mergeAgentFilters(SearchFilters{DateFrom: &from, DateTo: &to}, agentQuery{
		DateFrom: "2020-01-01", DateTo: "2030-01-01",
	})
	if !got.DateFrom.Equal(from) || !got.DateTo.Equal(to) {
		t.Fatalf("explicit filter was broadened: %v..%v", got.DateFrom, got.DateTo)
	}
}

func mustAgentDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
