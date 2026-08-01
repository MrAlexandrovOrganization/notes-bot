package search

import (
	"slices"
	"strings"
	"testing"
)

func TestParseDocument_NormalizesFrontmatter(t *testing.T) {
	src := `---
date: "[[09-Nov-2025]]"
title: Search design
tags: [rag, "#notes"]
link:
  - https://example.test/a
  - "[[Related note]]"
extra: preserved
---
# Body

Exact source text.
`
	doc := ParseDocument(src, "fallback")
	if doc.Metadata.Date == nil {
		t.Fatalf("unexpected date: %v", doc.Metadata.Date)
	}
	if got := doc.Metadata.Date.Format("2006-01-02"); got != "2025-11-09" {
		t.Fatalf("unexpected date: %s", got)
	}
	if doc.Metadata.Title != "Search design" {
		t.Fatalf("unexpected title: %q", doc.Metadata.Title)
	}
	if !slices.Equal(doc.Metadata.Tags, []string{"rag", "notes"}) {
		t.Fatalf("unexpected tags: %#v", doc.Metadata.Tags)
	}
	if len(doc.Metadata.Links) != 2 {
		t.Fatalf("unexpected links: %#v", doc.Metadata.Links)
	}
	if !strings.Contains(string(doc.Metadata.FrontmatterJSON), `"extra":"preserved"`) {
		t.Fatalf("raw frontmatter field was not retained: %s", doc.Metadata.FrontmatterJSON)
	}
	if doc.Body != "# Body\n\nExact source text." {
		t.Fatalf("unexpected body: %q", doc.Body)
	}
}

func TestParseDocument_UsesFilenameDateFallback(t *testing.T) {
	doc := ParseDocument("plain body", "31-Jul-2026")
	if doc.Metadata.Date == nil || doc.Metadata.Date.Format("2006-01-02") != "2026-07-31" {
		t.Fatalf("filename date was not parsed: %v", doc.Metadata.Date)
	}
}

func TestEmbeddingInputEnrichesRetrievalWithoutChangingChunkText(t *testing.T) {
	doc := ParseDocument("---\ntitle: My title\ntags: [one]\n---\nExact text", "note")
	chunk := Chunk{Kind: KindParagraph, Text: "Exact text", HeadingPath: "Section"}
	input := embeddingInput("note", doc.Metadata, chunk)
	for _, want := range []string{"Заголовок: My title", "Теги: one", "Раздел: Section", "Exact text"} {
		if !strings.Contains(input, want) {
			t.Fatalf("embedding input %q does not contain %q", input, want)
		}
	}
	if chunk.Text != "Exact text" {
		t.Fatalf("source chunk was modified: %q", chunk.Text)
	}
}
