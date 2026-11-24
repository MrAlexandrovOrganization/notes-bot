package english

import (
	"context"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notes "notes-bot/proto/notes"
)

type DictionaryMove struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	CardIDs []string `json:"card_ids"`
}

var cardIdentity = regexp.MustCompile(`^[a-f0-9]{64}$`)

// MoveDictionary serializes migrations with each other and active reviews.
func (s *Server) MoveDictionary(ctx context.Context, request DictionaryMove) error {
	return pgx.BeginFunc(ctx, s.Store.Pool, func(tx pgx.Tx) error {
		if _, _, err := loadSession(ctx, tx); err != nil {
			return err
		}
		return s.Dictionary.Move(ctx, request)
	})
}

// Move copies and verifies the destination before editing words.md. If the
// second write fails, both copies retain their original ID and remain usable.
func (d Dictionary) Move(ctx context.Context, request DictionaryMove) error {
	if !dictionaryID.MatchString(request.ID) || request.ID == allDictionaries || request.ID == generalDictionary ||
		strings.TrimSpace(request.Title) != request.Title || request.Title == "" ||
		strings.ContainsAny(request.Title, "\r\n\x00") || !utf8.ValidString(request.Title) || len([]rune(request.Title)) > 100 {
		return status.Error(codes.InvalidArgument, "invalid English dictionary")
	}
	want := make(map[string]bool, len(request.CardIDs))
	for _, id := range request.CardIDs {
		if !cardIdentity.MatchString(id) || want[id] {
			return status.Error(codes.InvalidArgument, "invalid or repeated English card ID")
		}
		want[id] = true
	}
	if len(want) == 0 {
		return status.Error(codes.InvalidArgument, "no English cards selected")
	}
	target := path.Join(dictionariesPath, request.ID+".md")
	for range 5 {
		err := d.moveDictionary(ctx, request, target, want)
		if status.Code(err) != codes.Aborted {
			return err
		}
	}
	return status.Error(codes.Aborted, "English dictionary changed concurrently")
}

func (d Dictionary) moveDictionary(ctx context.Context, request DictionaryMove, target string, want map[string]bool) error {
	snapshot, err := d.read(ctx)
	if err != nil {
		return err
	}
	text := snapshot.words.GetContent()
	// Startup normally pins the existing material. Pin only the selected IDs
	// when a new handwritten card is moved before its first grade.
	oldCards, err := parseEnglish(text, "word")
	if err != nil {
		return err
	}
	for _, card := range oldCards {
		if want[card.Id] && !strings.Contains(text, "<!-- notes-bot:english:"+card.Id+":") {
			text = markHandwritten(text, "word", card.Id)
		}
	}
	catalogue, err := snapshot.cards()
	if err != nil {
		return err
	}
	for _, card := range catalogue {
		if want[card.Id] && card.DictionaryId != generalDictionary && card.DictionaryId != request.ID {
			return status.Error(codes.FailedPrecondition, "a selected card belongs to another English dictionary")
		}
	}
	note, err := d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: target})
	if err != nil {
		return err
	}
	newCards, err := parseEnglish(note.GetContent(), "word")
	if err != nil {
		return err
	}
	existing := make(map[string]string, len(newCards))
	for _, card := range newCards {
		existing[card.Id] = englishPayload(card)
	}
	sources := make(map[string]bool, len(want))
	expected := make(map[string]string, len(want))
	for _, card := range oldCards {
		if !want[card.Id] {
			continue
		}
		sources[card.Id] = true
		expected[card.Id] = englishPayload(card)
		if payload, present := existing[card.Id]; present && payload != expected[card.Id] {
			return status.Error(codes.FailedPrecondition, "English copies differ; reconcile them in Obsidian")
		}
	}
	for id := range want {
		if !sources[id] && existing[id] == "" {
			return status.Error(codes.NotFound, "selected English card is absent from both dictionaries")
		}
	}
	if len(sources) == 0 {
		return nil
	}

	kept, undated, dated := splitDictionaryText(text, sources, existing)
	remaining, err := parseEnglish(kept, "word")
	if err != nil {
		return err
	}
	for _, card := range remaining {
		if want[card.Id] {
			return status.Error(codes.FailedPrecondition, "selected card has multiple entries in words.md")
		}
	}
	if undated != "" || dated != "" {
		dest := note.GetContent()
		if note.GetRevision() == "missing" {
			dest = "# " + request.Title + "\n"
		} else if dictionaryTitle(dictionaryFile{id: request.ID, note: note}) != request.Title {
			return status.Error(codes.FailedPrecondition, "English dictionary title differs")
		}
		dest = insertUndatedDictionaryText(dest, undated)
		if dated != "" {
			dest = strings.TrimRight(dest, "\n") + "\n\n" + dated
		}
		copied, err := parseEnglish(dest, "word")
		if err != nil {
			return err
		}
		for _, card := range copied {
			if payload, ok := expected[card.Id]; ok && englishPayload(card) == payload {
				delete(expected, card.Id)
			}
		}
		if len(expected) != 0 {
			return status.Error(codes.FailedPrecondition, "English dictionary move would change the material")
		}
		if err := d.replace(ctx, target, note, dest); err != nil {
			return err
		}
		note, err = d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: target})
		if err != nil {
			return err
		}
		if note.Content != dest {
			return status.Error(codes.Aborted, "destination changed during English dictionary move")
		}
	}
	return d.replace(ctx, englishWords, snapshot.words, kept)
}

func insertUndatedDictionaryText(text, entries string) string {
	if entries == "" {
		return text
	}
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		if englishDate.MatchString(line) {
			break
		}
		offset += len(line)
	}
	return strings.TrimRight(text[:offset], "\n") + "\n\n" + entries + "\n" + text[offset:]
}

// Keep the original entry and marker bytes, with the same logical date.
func splitDictionaryText(text string, selected map[string]bool, existing map[string]string) (string, string, string) {
	var kept, undated, dated strings.Builder
	var date, movedDate string
	var destination *strings.Builder
	moving := false
	for line := range strings.SplitAfterSeq(text, "\n") {
		if header := englishDate.FindStringSubmatch(line); header != nil {
			date = header[1]
			moving, destination = false, nil
			kept.WriteString(line)
			continue
		}
		if strings.HasPrefix(line, "  ") && moving {
			if destination != nil {
				destination.WriteString(line)
			}
			continue
		}
		if moving && strings.TrimSpace(line) == "" {
			if destination != nil {
				destination.WriteString(line)
			}
			moving, destination = false, nil
			continue
		}
		moving, destination = false, nil
		entry := strings.TrimSpace(line)
		marker := englishMarker.FindStringSubmatch(entry)
		if marker != nil && selected[marker[1]] {
			if _, _, ok := parseEnglishWord(strings.TrimSuffix(entry, marker[0])); ok {
				moving = true
				if existing[marker[1]] == "" {
					destination = &dated
					if date == "" {
						destination = &undated
					}
					if movedDate != date {
						destination.WriteString("[[" + date + "]]\n")
						movedDate = date
					}
					destination.WriteString(line)
				}
				continue
			}
		}
		kept.WriteString(line)
	}
	return kept.String(), undated.String(), dated.String()
}
