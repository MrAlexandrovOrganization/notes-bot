package english

import (
	"errors"
	"strings"

	"google.golang.org/protobuf/proto"

	pb "notes-bot/proto/english/v1"
)

func englishMigrationDone(words string) bool {
	for line := range strings.SplitSeq(words, "\n") {
		if strings.TrimSuffix(line, "\r") == englishPhrasesMigrated {
			return true
		}
	}
	return false
}

// Merge the legacy phrase list while retaining all card and operation IDs.
func mergeLegacyPhrases(words, phrases string) (string, int, error) {
	if englishMigrationDone(words) {
		return words, 0, nil
	}
	cards, err := parseEnglish(phrases, "phrase")
	if err != nil || len(cards) == 0 {
		return words, 0, err
	}

	payloadHashes := map[string]string{}
	for line := range strings.SplitSeq(phrases, "\n") {
		if marker := englishMarker.FindStringSubmatch(strings.TrimSpace(line)); marker != nil {
			payloadHashes[marker[1]] = marker[2]
		}
	}
	sections := make(map[string]*strings.Builder)
	var dates []string
	for _, card := range cards {
		hash := payloadHashes[card.Id]
		if hash == "" {
			hash = englishHash(englishPayload(card))
		}
		if sections[card.Date] == nil {
			sections[card.Date] = &strings.Builder{}
			dates = append(dates, card.Date)
		}
		sections[card.Date].WriteString(renderEnglish(card, hash))
	}
	merged := mergeEnglishSections(words, dates, sections)
	if err := verifyEnglishImport(merged, cards); err != nil {
		return "", 0, err
	}
	return merged, len(cards), nil
}

func mergeEnglishSections(words string, dates []string, sections map[string]*strings.Builder) string {
	var merged strings.Builder
	appendEntries := func(date string) {
		if entries := sections[date]; entries != nil {
			merged.WriteString("\n\n")
			merged.WriteString(entries.String())
			delete(sections, date)
		}
	}
	date := ""
	for line := range strings.SplitAfterSeq(words, "\n") {
		if match := englishDate.FindStringSubmatch(line); match != nil {
			appendEntries(date)
			date = match[1]
		}
		merged.WriteString(line)
	}
	appendEntries(date)
	for _, date := range dates {
		if entries := sections[date]; entries != nil {
			merged.WriteString("\n\n[[" + date + "]]\n")
			merged.WriteString(entries.String())
		}
	}
	merged.WriteString("\n" + englishPhrasesMigrated + "\n")
	return merged.String()
}

func verifyEnglishImport(text string, imported []*pb.Card) error {
	cards, err := parseEnglish(text, "word")
	if err != nil {
		return err
	}
	byID := make(map[string]*pb.Card, len(cards))
	for _, card := range cards {
		byID[card.Id] = card
	}
	for _, card := range imported {
		if !proto.Equal(card, byID[card.Id]) {
			return errors.New("migration could not preserve a card")
		}
	}
	return nil
}
