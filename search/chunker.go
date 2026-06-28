package search

import (
	"strings"
)

type ChunkKind string

const (
	KindNote      ChunkKind = "note"
	KindParagraph ChunkKind = "paragraph"
	KindTask      ChunkKind = "task"
)

type Chunk struct {
	Kind ChunkKind
	Ord  int
	Text string
}

// utf8BOM is the byte order mark sometimes prepended to UTF-8 files; we trim it
// before any structural parsing so frontmatter detection works.
const utf8BOM = "\ufeff"

// ChunkContent splits raw markdown into semantic chunks: one "note" chunk for
// the whole body (frontmatter stripped), one "task" chunk per task line, and one
// "paragraph" chunk per blank-line-separated paragraph in the body.
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
	chunks = append(chunks, Chunk{Kind: KindNote, Ord: 0, Text: body})

	taskOrd := 0
	for line := range strings.Lines(body) {
		t := strings.TrimSpace(line)
		if isTaskLine(t) {
			chunks = append(chunks, Chunk{Kind: KindTask, Ord: taskOrd, Text: t})
			taskOrd++
		}
	}

	paraOrd := 0
	for _, para := range splitParagraphs(body) {
		chunks = append(chunks, Chunk{Kind: KindParagraph, Ord: paraOrd, Text: para})
		paraOrd++
	}

	return chunks
}

// stripFrontmatter removes a YAML frontmatter block if the content starts with `---`.
// The block ends at the next line starting with `---`. Returns the content unchanged
// if no frontmatter is present.
func stripFrontmatter(content string) string {
	c := strings.TrimPrefix(content, utf8BOM)
	if !strings.HasPrefix(c, "---") {
		return content
	}
	rest := c[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return content
	}
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	after := rest[idx+len("\n---"):]
	after = strings.TrimLeft(after, "\r\n")
	return after
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

// splitParagraphs returns body split on blank lines. Empty paragraphs and
// pure-frontmatter-separator lines (`---`) are skipped.
func splitParagraphs(body string) []string {
	var paras []string
	var cur strings.Builder

	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s == "" || s == "---" {
			return
		}
		paras = append(paras, s)
	}

	for line := range strings.Lines(body) {
		stripped := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(stripped) == "" {
			flush()
			continue
		}
		cur.WriteString(stripped)
		cur.WriteByte('\n')
	}
	flush()
	return paras
}
