package search

import (
	"strings"
)

// Keep every embedding input comfortably below Ollama's context and physical
// batch limits. The limit is rune based so UTF-8 notes are never split in the
// middle of a character.
const maxChunkRunes = 1500

type ChunkKind string

const (
	// KindNote is retained for rolling-schema compatibility with old rows. The
	// current chunker never emits it.
	KindNote      ChunkKind = "note"
	KindParagraph ChunkKind = "paragraph"
	KindTask      ChunkKind = "task"
)

type Chunk struct {
	Kind        ChunkKind
	Ord         int
	Text        string
	HeadingPath string
}

// utf8BOM is the byte order mark sometimes prepended to UTF-8 files; we trim it
// before any structural parsing so frontmatter detection works.
const utf8BOM = "\ufeff"

// ChunkContent splits raw markdown into non-overlapping semantic chunks. Task
// lines become task chunks and are removed from paragraph chunks, so the same
// text never competes with itself under several kinds. Markdown headings are
// carried as metadata and ord is global within a note, which makes adjacent
// chunks addressable regardless of their kind.
//
// Returns nil if the body is empty after stripping. Order within each kind
// starts at 0 and matches reading order.
func ChunkContent(content string) []Chunk {
	body := stripFrontmatter(content)
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	chunks := make([]Chunk, 0, 8)
	headings := make([]string, 0, 6)
	var paragraph strings.Builder

	headingPath := func() string { return strings.Join(headings, " / ") }
	appendChunk := func(kind ChunkKind, text string) {
		for _, part := range splitLongText(text, maxChunkRunes) {
			chunks = append(chunks, Chunk{
				Kind:        kind,
				Ord:         len(chunks),
				Text:        part,
				HeadingPath: headingPath(),
			})
		}
	}
	flushParagraph := func() {
		text := strings.TrimSpace(paragraph.String())
		paragraph.Reset()
		if text == "" || text == "---" {
			return
		}
		appendChunk(KindParagraph, text)
	}

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if level, title, ok := markdownHeading(trimmed); ok {
			flushParagraph()
			if len(headings) >= level {
				headings = headings[:level-1]
			}
			for len(headings) < level-1 {
				headings = append(headings, "")
			}
			headings = append(headings, title)
			continue
		}
		if isTaskLine(trimmed) {
			flushParagraph()
			appendChunk(KindTask, trimmed)
			continue
		}
		paragraph.WriteString(line)
		paragraph.WriteByte('\n')
	}
	flushParagraph()

	return chunks
}

func markdownHeading(line string) (level int, title string, ok bool) {
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	title = strings.TrimSpace(strings.TrimRight(line[level+1:], "#"))
	return level, title, title != ""
}

// splitLongText splits at whitespace when possible and falls back to a hard
// rune boundary for generated/minified content without whitespace.
func splitLongText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return nil
	}

	runes := []rune(text)
	parts := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for len(runes) > maxRunes {
		cut := maxRunes
		for i := maxRunes; i > maxRunes/2; i-- {
			if strings.ContainsRune(" \t\r\n", runes[i-1]) {
				cut = i
				break
			}
		}
		part := strings.TrimSpace(string(runes[:cut]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = runes[cut:]
		for len(runes) > 0 && strings.ContainsRune(" \t\r\n", runes[0]) {
			runes = runes[1:]
		}
	}
	if part := strings.TrimSpace(string(runes)); part != "" {
		parts = append(parts, part)
	}
	return parts
}

// stripFrontmatter removes a YAML frontmatter block if the content starts with `---`.
// The block ends at the next line starting with `---`. Returns the content unchanged
// if no frontmatter is present.
func stripFrontmatter(content string) string {
	_, body, ok := splitFrontmatter(content)
	if !ok {
		return content
	}
	return body
}

func isTaskLine(s string) bool {
	if !strings.HasPrefix(s, "- [") {
		return false
	}
	if len(s) < 5 {
		return false
	}
	return s[4] == ']'
}
