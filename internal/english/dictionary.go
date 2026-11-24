package english

import (
	"context"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "notes-bot/proto/english/v1"
	notes "notes-bot/proto/notes"
)

type Markdown interface {
	ListDirectory(context.Context, *notes.ListDirectoryRequest, ...grpc.CallOption) (*notes.ListDirectoryResponse, error)
	ReadMarkdown(context.Context, *notes.ReadMarkdownRequest, ...grpc.CallOption) (*notes.ReadMarkdownResponse, error)
	ReplaceMarkdown(context.Context, *notes.ReplaceMarkdownRequest, ...grpc.CallOption) (*notes.ReplaceMarkdownResponse, error)
}

type Dictionary struct {
	Notes Markdown
}

type dictionarySnapshot struct {
	words   *notes.ReadMarkdownResponse
	phrases *notes.ReadMarkdownResponse
	themed  []dictionaryFile
}

type dictionaryFile struct {
	path string
	kind string
	note *notes.ReadMarkdownResponse
	id   string
}

const (
	allDictionaries   = "all"
	generalDictionary = "general"
	dictionariesPath  = "Tags/English/Dictionaries"
)

var dictionaryID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func (s dictionarySnapshot) files() []dictionaryFile {
	return append([]dictionaryFile{
		{englishWords, "word", s.words, generalDictionary},
		{englishPhrases, "phrase", s.phrases, generalDictionary},
	}, s.themed...)
}

func (d Dictionary) read(ctx context.Context) (dictionarySnapshot, error) {
	words, err := d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: englishWords})
	if err != nil {
		return dictionarySnapshot{}, err
	}
	snapshot := dictionarySnapshot{words: words}
	if !englishMigrationDone(words.Content) {
		snapshot.phrases, err = d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: englishPhrases})
		if err != nil {
			return dictionarySnapshot{}, err
		}
	}
	directory, err := d.Notes.ListDirectory(ctx, &notes.ListDirectoryRequest{Relpath: dictionariesPath})
	if status.Code(err) == codes.NotFound {
		return snapshot, nil
	}
	if err != nil {
		return dictionarySnapshot{}, err
	}
	for _, entry := range directory.GetEntries() {
		id, markdown := strings.CutSuffix(entry.GetName(), ".md")
		if entry.GetIsDir() || !markdown || !dictionaryID.MatchString(id) || id == allDictionaries || id == generalDictionary {
			continue
		}
		file := path.Join(dictionariesPath, entry.Name)
		note, err := d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: file})
		if err != nil {
			return dictionarySnapshot{}, err
		}
		if note.GetRevision() != "missing" {
			snapshot.themed = append(snapshot.themed, dictionaryFile{file, "word", note, id})
		}
	}
	slices.SortFunc(snapshot.themed, func(a, b dictionaryFile) int { return strings.Compare(a.id, b.id) })
	return snapshot, nil
}

func (snapshot dictionarySnapshot) cards() ([]*pb.Card, error) {
	var cards []*pb.Card
	seen := make(map[string]int)
	for _, file := range snapshot.files() {
		entries, err := parseEnglish(file.note.GetContent(), file.kind)
		if err != nil {
			return nil, err
		}
		for _, c := range entries {
			c.DictionaryId = file.id
			if i, ok := seen[c.Id]; ok {
				// A move can safely copy a card before removing it from words.md.
				if file.id == generalDictionary || cards[i].DictionaryId != generalDictionary ||
					englishPayload(cards[i]) != englishPayload(c) {
					return nil, errors.New("duplicate English identity across dictionary files")
				}
				cards[i] = c
				continue
			}
			seen[c.Id] = len(cards)
			cards = append(cards, c)
		}
	}
	return cards, nil
}

func dictionaryTitle(file dictionaryFile) string {
	for line := range strings.SplitSeq(file.note.GetContent(), "\n") {
		if title, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(title) != "" {
			return strings.TrimSpace(title)
		}
	}
	return file.id
}

func (snapshot dictionarySnapshot) dictionaries(cards []*pb.Card) []*pb.DictionaryInfo {
	counts := make(map[string]int32)
	for _, c := range cards {
		counts[c.DictionaryId]++
	}
	result := []*pb.DictionaryInfo{
		{Id: allDictionaries, Title: "Все слова", CardCount: int32(len(cards))},
		{Id: generalDictionary, Title: "Основной", CardCount: counts[generalDictionary]},
	}
	for _, file := range snapshot.themed {
		result = append(result, &pb.DictionaryInfo{Id: file.id, Title: dictionaryTitle(file), CardCount: counts[file.id]})
	}
	return result
}

func (snapshot dictionarySnapshot) selectDictionary(id string) (string, error) {
	switch id {
	case "", allDictionaries:
		return allDictionaries, nil
	case generalDictionary:
		return generalDictionary, nil
	}
	for _, file := range snapshot.themed {
		if file.id == id {
			return id, nil
		}
	}
	return "", status.Error(codes.NotFound, "English dictionary not found")
}

func (d Dictionary) replace(ctx context.Context, path string, previous *notes.ReadMarkdownResponse, text string) error {
	_, err := d.Notes.ReplaceMarkdown(ctx, &notes.ReplaceMarkdownRequest{
		Relpath: path, ExpectedRevision: previous.Revision, Content: text,
	})
	return err
}

func (d Dictionary) Migrate(ctx context.Context) error {
	for range 5 {
		snapshot, err := d.read(ctx)
		if err != nil {
			return err
		}
		text, _, err := mergeLegacyPhrases(snapshot.words.GetContent(), snapshot.phrases.GetContent())
		if err != nil {
			return err
		}
		if _, err := snapshot.cards(); err != nil {
			return err
		}
		text = markHandwritten(text, "word", "")
		if text == snapshot.words.GetContent() {
			return nil
		}
		err = d.replace(ctx, englishWords, snapshot.words, text)
		if status.Code(err) != codes.Aborted {
			return err
		}
	}
	return status.Error(codes.Aborted, "English dictionary changed concurrently")
}

var operationPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func (d Dictionary) save(ctx context.Context, req *pb.SaveCardRequest) (*pb.Card, error) {
	if !operationPattern.MatchString(req.GetOperationId()) || !validateEnglishCard(req.GetCard()) {
		return nil, status.Error(codes.InvalidArgument, "invalid English card")
	}
	card := proto.CloneOf(req.GetCard())
	date, err := time.Parse("02-Jan-2006", card.Date)
	if err != nil || date.Format("02-Jan-2006") != card.Date {
		return nil, status.Error(codes.InvalidArgument, "invalid date")
	}
	card.Id = englishHash("english:" + req.OperationId)
	card.DictionaryId = generalDictionary
	hash := englishHash(englishPayload(card))
attempt:
	for range 5 {
		snapshot, err := d.read(ctx)
		if err != nil {
			return nil, err
		}
		for _, file := range snapshot.files() {
			for line := range strings.SplitSeq(file.note.GetContent(), "\n") {
				marker := englishMarker.FindStringSubmatch(strings.TrimSpace(line))
				if marker == nil || marker[1] != card.Id {
					continue
				}
				if marker[2] != hash {
					return nil, status.Error(codes.AlreadyExists, "English save operation conflict")
				}
				cards, err := snapshot.cards()
				if err != nil {
					return nil, err
				}
				for _, stored := range cards {
					if stored.Id == card.Id {
						// A prior replacement could have renamed the note before
						// losing its reply. Confirm the file is durable on retry.
						err := d.replace(ctx, file.path, file.note, file.note.Content)
						if status.Code(err) == codes.Aborted {
							continue attempt
						}
						return stored, err
					}
				}
				return nil, status.Error(codes.FailedPrecondition, "saved card was edited")
			}
		}
		text := insertEnglish(snapshot.words.Content, card.Date, renderEnglish(card, hash))
		err = d.replace(ctx, englishWords, snapshot.words, text)
		if status.Code(err) != codes.Aborted {
			return card, err
		}
	}
	return nil, status.Error(codes.Aborted, "English dictionary changed concurrently")
}

// pin retains the identity across future edits in Obsidian.
func (d Dictionary) pin(ctx context.Context, snapshot dictionarySnapshot, id string) error {
	marker := "<!-- notes-bot:english:" + id + ":"
	for _, file := range snapshot.files() {
		if strings.Contains(file.note.GetContent(), marker) {
			return nil
		}
	}
	for _, file := range snapshot.files() {
		text := markHandwritten(file.note.GetContent(), file.kind, id)
		if text != file.note.GetContent() {
			err := d.replace(ctx, file.path, file.note, text)
			if status.Code(err) == codes.Aborted {
				return status.Error(codes.FailedPrecondition, "English dictionary changed")
			}
			return err
		}
	}
	return nil
}
