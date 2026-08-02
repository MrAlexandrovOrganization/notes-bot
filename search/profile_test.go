package search

import (
	"strings"
	"testing"
)

func TestNormalizeProfileDropsInvalidEvidenceAndDeduplicatesPeople(t *testing.T) {
	profile := NoteProfile{
		People: []string{"Маша", " маша ", ""},
		Activities: []ProfileItem{{
			Text: " Зал ", People: []string{"Саша", "саша"}, EvidenceOrd: []int{2, -1, 2, 7, 1},
		}},
	}
	normalizeProfile(&profile, 3)
	if len(profile.People) != 1 || profile.People[0] != "Маша" {
		t.Fatalf("people were not normalized: %#v", profile.People)
	}
	item := profile.Activities[0]
	if item.Text != "Зал" || len(item.People) != 1 {
		t.Fatalf("item was not normalized: %#v", item)
	}
	if len(item.EvidenceOrd) != 2 || item.EvidenceOrd[0] != 1 || item.EvidenceOrd[1] != 2 {
		t.Fatalf("unexpected evidence ords: %#v", item.EvidenceOrd)
	}
}

func TestProfileSourceLabelsRawChunks(t *testing.T) {
	note := NoteFull{NoteRow: NoteRow{Name: "01-Aug-2026"}}
	source := profileSource(note, []Chunk{{Kind: KindTask, Ord: 3, HeadingPath: "День", Text: "- [x] Зал"}}, 1000)
	for _, want := range []string{"Заметка: 01-Aug-2026", "[3 | task | День]", "- [x] Зал"} {
		if !strings.Contains(source, want) {
			t.Fatalf("profile source %q does not contain %q", source, want)
		}
	}
}

func TestProfileSourcesCoverEveryChunkWithoutTruncation(t *testing.T) {
	note := NoteFull{NoteRow: NoteRow{Name: "long"}}
	chunks := []Chunk{
		{Kind: KindParagraph, Ord: 0, Text: strings.Repeat("a", 80)},
		{Kind: KindParagraph, Ord: 1, Text: strings.Repeat("b", 80)},
		{Kind: KindParagraph, Ord: 2, Text: strings.Repeat("c", 80)},
	}
	sources := profileSources(note, chunks, 180)
	if len(sources) < 2 {
		t.Fatalf("long note was not split: %#v", sources)
	}
	joined := strings.Join(sources, "\n")
	for _, ord := range []string{"[0 |", "[1 |", "[2 |"} {
		if strings.Count(joined, ord) != 1 {
			t.Fatalf("chunk %s missing or duplicated in %q", ord, joined)
		}
	}
}
