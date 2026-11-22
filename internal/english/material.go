package english

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	pb "notes-bot/proto/english/v1"
)

const englishWords = "Tags/English/words.md"
const englishPhrases = "Tags/English/phrases.md"
const englishPhrasesMigrated = "<!-- notes-bot:english-phrases-migrated -->"

var englishDate = regexp.MustCompile(`^\s*(?:#{1,6}\s*)?\[\[([0-9]{2}-[A-Za-z]{3}-[0-9]{4})\]\]\s*$`)
var englishWord = regexp.MustCompile("^[-*] +(`+)")
var englishSeparator = regexp.MustCompile(`^ +[-–—] +`)
var englishMarker = regexp.MustCompile(` <!-- notes-bot:english:([a-f0-9]{64}):([a-f0-9]{64})(?::(phrase))? -->$`)

func englishHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// Markdown is the only material store. Unmarked handwritten entries have a
// deterministic content identity; bot entries retain an ID through manual edits.
// No read, including the first read of the old dictionary, rewrites the vault.
func parseEnglish(text, kind string) ([]*pb.Card, error) {
	var cards []*pb.Card
	var current *pb.Card
	date := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if m := englishDate.FindStringSubmatch(line); m != nil {
			date = m[1]
			current = nil
			continue
		}
		if strings.HasPrefix(line, "  ") && current != nil {
			detail := strings.TrimSpace(line)
			for prefix, target := range map[string]*string{"Пояснение: ": &current.Explanation, "Пример: ": &current.Example, "Контекст: ": &current.Context} {
				if rest, ok := strings.CutPrefix(detail, prefix); ok {
					*target = rest
				}
			}
			continue
		}
		current = nil
		raw := strings.TrimSpace(line)
		marker := englishMarker.FindStringSubmatch(raw)
		if marker != nil {
			raw = strings.TrimSuffix(raw, marker[0])
		}
		c := &pb.Card{Kind: kind, Date: date}
		if kind == "word" {
			var ok bool
			c.English, c.Source, ok = parseEnglishWord(raw)
			if !ok {
				continue
			}
		} else {
			if !strings.HasPrefix(raw, "- ") && !strings.HasPrefix(raw, "* ") {
				continue
			}
			source, en, ok := strings.Cut(raw[2:], " → ")
			if !ok {
				continue
			}
			c.Source = source
			c.English = en
		}
		c.Source = strings.TrimSpace(c.Source)
		c.English = strings.TrimSpace(c.English)
		if c.Source == "" || c.English == "" {
			continue
		}
		c.Id = englishHash(kind + "\x00" + date + "\x00" + c.Source + "\x00" + c.English)
		if marker != nil {
			c.Id = marker[1]
			if marker[3] != "" {
				c.Kind = marker[3]
			}
		}
		if seen[c.Id] { // Repeated identical handwritten lines are one card.
			if marker != nil {
				return nil, fmt.Errorf("duplicate English identity")
			}
			continue
		}
		seen[c.Id] = true
		cards = append(cards, c)
		current = c
	}
	return cards, nil
}

func parseEnglishWord(line string) (english, source string, ok bool) {
	match := englishWord.FindStringSubmatch(line)
	if match == nil {
		return "", "", false
	}
	english, rest, found := strings.Cut(line[len(match[0]):], match[1])
	separator := englishSeparator.FindString(rest)
	if !found || separator == "" {
		return "", "", false
	}
	return english, rest[len(separator):], true
}

func englishPayload(c *pb.Card) string {
	return strings.Join([]string{c.Kind, c.Date, c.Source, c.English, c.Explanation, c.Example, c.Context}, "\x00")
}

func validateEnglishCard(c *pb.Card) bool {
	if c == nil || (c.Kind != "word" && c.Kind != "phrase") || strings.TrimSpace(c.Source) == "" || strings.TrimSpace(c.English) == "" {
		return false
	}
	for _, s := range []string{c.Source, c.English, c.Explanation, c.Example, c.Context} {
		if !utf8.ValidString(s) || len([]rune(s)) > 600 || strings.ContainsAny(s, "\r\n\x00") || strings.Contains(s, "<!--") {
			return false
		}
	}
	return !strings.Contains(c.English, "`") && !strings.Contains(c.Source, " → ")
}

func renderEnglish(c *pb.Card, payloadHash string) string {
	fenceSize := 1
	for run := range strings.FieldsFuncSeq(c.English, func(r rune) bool { return r != '`' }) {
		fenceSize = max(fenceSize, len(run)+1)
	}
	fence := strings.Repeat("`", fenceSize)
	english := c.English
	if len(fence) > 1 {
		english = " " + english + " "
	}
	line := "- " + fence + english + fence + " - " + c.Source
	marker := c.Id + ":" + payloadHash
	if c.Kind == "phrase" {
		// Preserve the old wire kind and revision through a format change.
		marker += ":phrase"
	}
	line += " <!-- notes-bot:english:" + marker + " -->\n"
	for _, field := range [][2]string{{"Пояснение", c.Explanation}, {"Пример", c.Example}, {"Контекст", c.Context}} {
		if field[1] != "" {
			line += "  " + field[0] + ": " + field[1] + "\n"
		}
	}
	return line + "\n"
}

// Place the new entry inside its date section, preserving every existing byte.
func insertEnglish(text, date, entry string) string {
	start := -1
	offset := 0
	insert := len(text)
	for line := range strings.SplitAfterSeq(text, "\n") {
		if m := englishDate.FindStringSubmatch(strings.TrimSuffix(line, "\n")); m != nil {
			if start >= 0 {
				insert = offset
				break
			}
			if m[1] == date {
				start = offset
			}
		}
		offset += len(line)
	}
	prefix := ""
	if start < 0 {
		prefix = "\n[[" + date + "]]\n"
	}
	if insert > 0 && text[insert-1] != '\n' {
		prefix = "\n" + prefix
	}
	return text[:insert] + prefix + entry + text[insert:]
}

// Preserve the existing content-derived identity when a handwritten entry first
// gets persistent progress. The visible text and its line endings are retained.
func markHandwritten(text, kind, onlyID string) string {
	lines := strings.Split(text, "\n")
	header := ""
	seen := make(map[string]bool)
	for i, line := range lines {
		if englishDate.MatchString(line) {
			header = line + "\n"
			continue
		}
		if strings.HasPrefix(line, "  ") {
			continue
		}
		cards, _ := parseEnglish(header+line, kind)
		if len(cards) != 1 {
			continue
		}
		card := cards[0]
		alreadySeen := seen[card.Id]
		seen[card.Id] = true
		if alreadySeen || englishMarker.MatchString(strings.TrimSpace(line)) || (onlyID != "" && card.Id != onlyID) {
			continue
		}
		trimmed := strings.TrimRight(line, " \t\r")
		lines[i] = trimmed + fmt.Sprintf(" <!-- notes-bot:english:%s:%s -->", card.Id, englishHash(englishPayload(card))) + line[len(trimmed):]
	}
	return strings.Join(lines, "\n")
}
