package search

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// NoteMetadata is the normalized, searchable subset of YAML frontmatter. The
// complete decoded frontmatter is also retained as JSON for future features.
type NoteMetadata struct {
	Date            *time.Time
	Title           string
	Tags            []string
	Links           []string
	FrontmatterJSON []byte
}

type ParsedDocument struct {
	Body     string
	Metadata NoteMetadata
}

func ParseDocument(content, fallbackName string) ParsedDocument {
	header, body, hasFrontmatter := splitFrontmatter(content)
	metadata := NoteMetadata{FrontmatterJSON: []byte("{}")}

	if hasFrontmatter {
		values := make(map[string]any)
		if err := yaml.Unmarshal([]byte(header), &values); err != nil {
			values = parseSimpleFrontmatter(header)
		}
		if encoded, err := json.Marshal(values); err == nil {
			metadata.FrontmatterJSON = encoded
		}
		metadata.Title = scalarString(values["title"])
		metadata.Tags = normalizeStringList(values["tags"])
		metadata.Links = appendUnique(nil, normalizeStringList(values["link"])...)
		metadata.Links = appendUnique(metadata.Links, normalizeStringList(values["links"])...)
		metadata.Date = metadataDate(values["date"])
	}

	if metadata.Date == nil {
		metadata.Date = parseNoteDate(fallbackName)
	}
	return ParsedDocument{Body: strings.TrimSpace(body), Metadata: metadata}
}

// splitFrontmatter preserves the note body byte-for-byte apart from removing
// the opening/closing YAML delimiter and leading line breaks after it.
func splitFrontmatter(content string) (header, body string, ok bool) {
	c := strings.TrimPrefix(content, utf8BOM)
	firstEnd := strings.IndexByte(c, '\n')
	if firstEnd < 0 || strings.TrimSuffix(c[:firstEnd], "\r") != "---" {
		return "", content, false
	}

	lineStart := firstEnd + 1
	for lineStart <= len(c) {
		lineEnd := strings.IndexByte(c[lineStart:], '\n')
		next := len(c)
		if lineEnd >= 0 {
			lineEnd += lineStart
			next = lineEnd + 1
		} else {
			lineEnd = len(c)
		}
		line := strings.TrimSuffix(c[lineStart:lineEnd], "\r")
		if line == "---" || line == "..." {
			return c[firstEnd+1 : lineStart], strings.TrimLeft(c[next:], "\r\n"), true
		}
		if next >= len(c) {
			break
		}
		lineStart = next
	}
	return "", content, false
}

func parseSimpleFrontmatter(header string) map[string]any {
	values := make(map[string]any)
	for line := range strings.Lines(header) {
		line = strings.TrimSpace(line)
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) == "" {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values
}

func scalarString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func normalizeStringList(value any) []string {
	var raw []string
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		for _, item := range v {
			raw = append(raw, scalarString(item))
		}
	case []string:
		raw = append(raw, v...)
	default:
		s := scalarString(v)
		if strings.Contains(s, ",") {
			raw = append(raw, strings.Split(s, ",")...)
		} else {
			raw = append(raw, s)
		}
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		item = strings.TrimPrefix(item, "#")
		if item != "" {
			out = appendUnique(out, item)
		}
	}
	return out
}

func appendUnique(dst []string, values ...string) []string {
	for _, value := range values {
		if value == "" {
			continue
		}
		seen := false
		for _, existing := range dst {
			if existing == value {
				seen = true
				break
			}
		}
		if !seen {
			dst = append(dst, value)
		}
	}
	return dst
}

func metadataDate(value any) *time.Time {
	if t, ok := value.(time.Time); ok {
		date := dateOnly(t)
		return &date
	}
	return parseNoteDate(scalarString(value))
}

func parseNoteDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[[")
	value = strings.TrimSuffix(value, "]]")
	for _, layout := range []string{"02-Jan-2006", "2006-01-02", "02.01.2006"} {
		if t, err := time.Parse(layout, value); err == nil {
			date := dateOnly(t)
			return &date
		}
	}
	return nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func embeddingInput(noteName string, metadata NoteMetadata, chunk Chunk) string {
	title := metadata.Title
	if title == "" {
		title = noteName
	}
	parts := make([]string, 0, 5)
	if title != "" {
		parts = append(parts, "Заголовок: "+title)
	}
	if metadata.Date != nil {
		parts = append(parts, "Дата: "+metadata.Date.Format("2006-01-02"))
	}
	if len(metadata.Tags) > 0 {
		parts = append(parts, "Теги: "+strings.Join(metadata.Tags, ", "))
	}
	if len(metadata.Links) > 0 {
		parts = append(parts, "Ссылки: "+strings.Join(metadata.Links, ", "))
	}
	if chunk.HeadingPath != "" {
		parts = append(parts, "Раздел: "+chunk.HeadingPath)
	}
	parts = append(parts, chunk.Text)
	return strings.Join(parts, "\n")
}
