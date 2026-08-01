package search

import (
	"slices"
	"strings"
	"testing"
)

func TestChunkContent_StripsFrontmatter(t *testing.T) {
	src := `---
date: "[[09-Nov-2025]]"
title: hello
Оценка: 8
---
- [ ] Task one
- [x] Task two [completion:: 2025-03-07]
---

First paragraph text.

Second paragraph
with two lines.
`
	chunks := ChunkContent(src)
	if len(chunks) == 0 {
		t.Fatalf("expected non-empty chunks")
	}
	if chunks[0].Kind != KindNote {
		t.Fatalf("first chunk should be KindNote, got %s", chunks[0].Kind)
	}
	if strings.Contains(chunks[0].Text, "date:") || strings.Contains(chunks[0].Text, "Оценка:") {
		t.Errorf("note chunk should not include frontmatter, got: %q", chunks[0].Text)
	}

	gotTasks, gotParas := 0, 0
	for _, c := range chunks[1:] {
		switch c.Kind {
		case KindTask:
			gotTasks++
		case KindParagraph:
			gotParas++
		}
	}
	if gotTasks != 2 {
		t.Errorf("want 2 task chunks, got %d", gotTasks)
	}
	if gotParas < 2 {
		t.Errorf("want at least 2 paragraph chunks, got %d", gotParas)
	}
}

func TestChunkContent_NoFrontmatter(t *testing.T) {
	src := "Just some text\n\nAnother paragraph"
	chunks := ChunkContent(src)
	if len(chunks) == 0 || chunks[0].Kind != KindNote {
		t.Fatalf("expected a note chunk, got %#v", chunks)
	}
	kinds := make([]ChunkKind, 0, len(chunks))
	for _, c := range chunks {
		kinds = append(kinds, c.Kind)
	}
	if !slices.Contains(kinds, KindParagraph) {
		t.Errorf("expected paragraph chunks, got kinds %v", kinds)
	}
}

func TestChunkContent_Empty(t *testing.T) {
	if got := ChunkContent(""); got != nil {
		t.Errorf("want nil chunks for empty input, got %#v", got)
	}
	if got := ChunkContent("---\nfoo: bar\n---\n"); got != nil {
		t.Errorf("want nil chunks for frontmatter-only input, got %#v", got)
	}
}

func TestChunkContent_Ordering(t *testing.T) {
	src := `- [ ] A
- [x] B
- [ ] C

Paragraph X.`
	chunks := ChunkContent(src)
	var tasks []string
	for _, c := range chunks {
		if c.Kind == KindTask {
			tasks = append(tasks, c.Text)
		}
	}
	want := []string{"- [ ] A", "- [x] B", "- [ ] C"}
	if !slices.Equal(tasks, want) {
		t.Errorf("task order mismatch: got %v, want %v", tasks, want)
	}
}

func TestChunkContent_SplitsLargeInputs(t *testing.T) {
	src := strings.Repeat("длинный-текст ", 1000)
	chunks := ChunkContent(src)
	if len(chunks) < 2 {
		t.Fatalf("expected large content to be split, got %d chunk(s)", len(chunks))
	}
	for _, chunk := range chunks {
		if got := len([]rune(chunk.Text)); got > maxChunkRunes {
			t.Errorf("%s/%d has %d runes, limit is %d", chunk.Kind, chunk.Ord, got, maxChunkRunes)
		}
	}
}

func TestSplitLongText_NoWhitespace(t *testing.T) {
	parts := splitLongText(strings.Repeat("я", maxChunkRunes+1), maxChunkRunes)
	if len(parts) != 2 {
		t.Fatalf("want 2 parts, got %d", len(parts))
	}
	if len([]rune(parts[0])) != maxChunkRunes || len([]rune(parts[1])) != 1 {
		t.Fatalf("unexpected part sizes: %d, %d", len([]rune(parts[0])), len([]rune(parts[1])))
	}
}

func TestStripFrontmatter_CRLF(t *testing.T) {
	src := "---\r\nfoo: 1\r\n---\r\nbody"
	got := stripFrontmatter(src)
	if strings.Contains(got, "foo") {
		t.Errorf("CRLF frontmatter not stripped: %q", got)
	}
	if !strings.Contains(got, "body") {
		t.Errorf("body missing after strip: %q", got)
	}
}
