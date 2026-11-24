package english

import (
	"context"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/english/v1"
	notes "notes-bot/proto/notes"
)

type testMarkdown struct {
	mu    sync.Mutex
	files map[string]string
}

func (m *testMarkdown) ListDirectory(_ context.Context, req *notes.ListDirectoryRequest, _ ...grpc.CallOption) (*notes.ListDirectoryResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := &notes.ListDirectoryResponse{}
	for file := range m.files {
		if path.Dir(file) == req.GetRelpath() {
			list.Entries = append(list.Entries, &notes.DirEntry{Name: path.Base(file), Relpath: file})
		}
	}
	if len(list.Entries) == 0 {
		return nil, status.Error(codes.NotFound, "directory missing")
	}
	return list, nil
}

func (m *testMarkdown) ReadMarkdown(_ context.Context, req *notes.ReadMarkdownRequest, _ ...grpc.CallOption) (*notes.ReadMarkdownResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &notes.ReadMarkdownResponse{Content: m.files[req.Relpath], Revision: m.revision(req.Relpath)}, nil
}

func (m *testMarkdown) revision(file string) string {
	if text, ok := m.files[file]; ok {
		return englishHash(text)
	}
	return "missing"
}

func (m *testMarkdown) ReplaceMarkdown(_ context.Context, req *notes.ReplaceMarkdownRequest, _ ...grpc.CallOption) (*notes.ReplaceMarkdownResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revision(req.Relpath) != req.ExpectedRevision {
		return nil, status.Error(codes.Aborted, "stale")
	}
	m.files[req.Relpath] = req.Content
	return &notes.ReplaceMarkdownResponse{}, nil
}

func TestMoveDictionaryKeepsCardIDsDatesAndTitles(t *testing.T) {
	source := "---\ndate:\n---\n- `undated` - без даты\n\n[[22-Nov-2026]]\n" +
		"- `one` - один\n  Пояснение: первое\n  Пример: One more.\n- `two` - два\n"
	files := &testMarkdown{files: map[string]string{englishWords: source}}
	service := &Server{Dictionary: Dictionary{Notes: files}}
	ctx := t.Context()
	before, err := service.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, before.Cards, 3)
	move := DictionaryMove{ID: "movie", Title: "Название фильма", CardIDs: []string{before.Cards[1].Id}}
	require.NoError(t, service.Dictionary.Move(ctx, move))
	move.CardIDs = append(move.CardIDs, before.Cards[0].Id)
	require.NoError(t, service.Dictionary.Move(ctx, move))
	require.NoError(t, service.Dictionary.Move(ctx, move))
	cards, err := service.ListCards(ctx, &pb.ListCardsRequest{DictionaryId: move.ID})
	require.NoError(t, err)
	require.Len(t, cards.Cards, 2)
	require.Equal(t, before.Cards[0].Id, cards.Cards[0].Id)
	require.Equal(t, before.Cards[0].Revision, cards.Cards[0].Revision)
	require.Equal(t, before.Cards[1].Id, cards.Cards[1].Id)
	require.Equal(t, before.Cards[1].Revision, cards.Cards[1].Revision)
	require.Equal(t, source[:strings.Index(source, "- `undated`")]+"[[22-Nov-2026]]\n- `two` - два\n", files.files[englishWords])
	require.Equal(t, move.Title, cards.Dictionaries[2].Title)
	require.EqualValues(t, 3, cards.Dictionaries[0].CardCount)
	require.EqualValues(t, 1, cards.Dictionaries[1].CardCount)
	require.EqualValues(t, 2, cards.Dictionaries[2].CardCount)
	move.ID = "other"
	require.Equal(t, codes.FailedPrecondition, status.Code(service.Dictionary.Move(ctx, move)))
}

func TestEnglishDictionaryMigratesPhrasesAndKeepsManualIdentity(t *testing.T) {
	files := &testMarkdown{files: map[string]string{
		englishWords:   "[[21-Nov-2026]]\n- `cliff` - утёс\n",
		englishPhrases: "[[22-Nov-2026]]\n- Я уже в пути → I'm on my way.\n  Пример: Text me when you're there.\n",
	}}
	ctx := t.Context()
	server := &Server{Dictionary: Dictionary{Notes: files}}
	before, err := server.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, before.Cards, 2)
	require.NotContains(t, files.files[englishWords], "notes-bot:english")
	require.NoError(t, server.Dictionary.Migrate(ctx))
	merged := files.files[englishWords]
	require.NoError(t, server.Dictionary.Migrate(ctx))
	require.Equal(t, merged, files.files[englishWords])
	require.Contains(t, merged, englishPhrasesMigrated)
	files.files[englishWords] = strings.Replace(merged, "утёс", "обрыв", 1)
	after, err := server.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, after.Cards, 2)
	require.Equal(t, before.Cards[0].Id, after.Cards[0].Id)
	require.Equal(t, "обрыв", after.Cards[0].Source)
	require.Equal(t, before.Cards[1].Id, after.Cards[1].Id)
	require.Equal(t, before.Cards[1].Example, after.Cards[1].Example)
}

func TestEnglishConcurrentSavesAndRetriesPreserveMarkdown(t *testing.T) {
	prior := "[[20-Nov-2026]]\n- `before` - до\n\n[[22-Nov-2026]]\n- `after` - после\n"
	files := &testMarkdown{files: map[string]string{englishWords: prior}}
	server := &Server{Dictionary: Dictionary{Notes: files}}
	ctx := t.Context()
	makeRequest := func(id string) *pb.SaveCardRequest {
		return &pb.SaveCardRequest{OperationId: id, Card: &pb.Card{
			Kind: "word", Date: "20-Nov-2026", English: id, Source: "перевод",
		}}
	}
	errs := make(chan error, 3)
	for _, id := range []string{"apple", "pear", "apple"} {
		go func() {
			_, err := server.SaveCard(ctx, makeRequest(id))
			errs <- err
		}()
	}
	for range 3 {
		require.NoError(t, <-errs)
	}
	text := files.files[englishWords]
	require.Contains(t, text, "- `before` - до")
	require.Equal(t, 1, strings.Count(text, "`apple`"))
	require.Equal(t, 1, strings.Count(text, "`pear`"))
	require.Less(t, strings.Index(text, "`apple`"), strings.Index(text, "[[22-Nov-2026]]"))
	conflict := makeRequest("apple")
	conflict.Card.Source = "другой перевод"
	_, err := server.SaveCard(ctx, conflict)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}
