package tghandlers

import (
	"strings"
	"testing"

	"notes-bot/frontends/telegram/clients"
)

func TestBuildAskContextIncludesStructuredMetadataAndDeduplicatesChunks(t *testing.T) {
	hit := &clients.SearchHit{
		ChunkID: 7, Name: "31-Jul-2026", NoteDate: "2026-07-31",
		Title: "Daily", Tags: []string{"work"}, Links: []string{"[[Project]]"},
		Heading: "Decisions", Snippet: "Exact source sentence.",
	}
	context, sources := buildAskContext([]*clients.SearchHit{hit, hit})
	for _, want := range []string{
		"[2026-07-31 · Decisions]", "Заголовок: Daily", "Теги: work",
		"Ссылки: [[Project]]", "Exact source sentence.",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("context %q does not contain %q", context, want)
		}
	}
	if strings.Count(context, "Exact source sentence.") != 1 {
		t.Fatalf("duplicate chunk was included: %q", context)
	}
	if len(sources) != 1 || sources[0] != "31-Jul-2026" {
		t.Fatalf("unexpected sources: %#v", sources)
	}
}
