package english

import (
	"context"
	"errors"
	"regexp"
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
	ReadMarkdown(context.Context, *notes.ReadMarkdownRequest, ...grpc.CallOption) (*notes.ReadMarkdownResponse, error)
	ReplaceMarkdown(context.Context, *notes.ReplaceMarkdownRequest, ...grpc.CallOption) (*notes.ReplaceMarkdownResponse, error)
}

type Dictionary struct {
	Notes Markdown
}

type dictionarySnapshot struct {
	words   *notes.ReadMarkdownResponse
	phrases *notes.ReadMarkdownResponse
}

type dictionaryFile struct {
	path string
	kind string
	note *notes.ReadMarkdownResponse
}

func (s dictionarySnapshot) files() [2]dictionaryFile {
	return [2]dictionaryFile{{englishWords, "word", s.words}, {englishPhrases, "phrase", s.phrases}}
}

func (d Dictionary) read(ctx context.Context) (dictionarySnapshot, error) {
	words, err := d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: englishWords})
	if err != nil {
		return dictionarySnapshot{}, err
	}
	snapshot := dictionarySnapshot{words: words}
	if !englishMigrationDone(words.Content) {
		snapshot.phrases, err = d.Notes.ReadMarkdown(ctx, &notes.ReadMarkdownRequest{Relpath: englishPhrases})
	}
	return snapshot, err
}

func (snapshot dictionarySnapshot) cards() ([]*pb.Card, error) {
	words, err := parseEnglish(snapshot.words.GetContent(), "word")
	if err != nil {
		return nil, err
	}
	phrases, err := parseEnglish(snapshot.phrases.GetContent(), "phrase")
	if err != nil {
		return nil, err
	}
	cards := append(words, phrases...)
	seen := make(map[string]bool, len(cards))
	for _, c := range cards {
		if seen[c.Id] {
			return nil, errors.New("duplicate English identity across files")
		}
		seen[c.Id] = true
	}
	return cards, nil
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
	if strings.Contains(snapshot.words.GetContent(), marker) || strings.Contains(snapshot.phrases.GetContent(), marker) {
		return nil
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
