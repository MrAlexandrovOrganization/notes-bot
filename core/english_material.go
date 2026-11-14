package core

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

var englishDate = regexp.MustCompile(`^\s*(?:#{1,6}\s*)?\[\[([0-9]{2}-[A-Za-z]{3}-[0-9]{4})\]\]\s*$`)
var englishWord = regexp.MustCompile("^[-*] +`([^`]+)` +[-–—] +(.+)$")
var englishMarker = regexp.MustCompile(` <!-- notes-bot:english:([a-f0-9]{64}):([a-f0-9]{64}) -->$`)

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
			m := englishWord.FindStringSubmatch(raw)
			if m == nil {
				continue
			}
			c.English = m[1]
			c.Source = m[2]
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
	line := "- " + c.Source + " → " + c.English
	if c.Kind == "word" {
		line = "- `" + c.English + "` - " + c.Source
	}
	line += " <!-- notes-bot:english:" + c.Id + ":" + payloadHash + " -->\n"
	for _, field := range [][2]string{{"Пояснение", c.Explanation}, {"Пример", c.Example}, {"Контекст", c.Context}} {
		if field[1] != "" {
			line += "  " + field[0] + ": " + field[1] + "\n"
		}
	}
	return line
}
