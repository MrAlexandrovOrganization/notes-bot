package english

import (
	"context"
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

func (m *testMarkdown) ReadMarkdown(_ context.Context, req *notes.ReadMarkdownRequest, _ ...grpc.CallOption) (*notes.ReadMarkdownResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	text := m.files[req.Relpath]
	return &notes.ReadMarkdownResponse{Content: text, Revision: englishHash(text)}, nil
}

func (m *testMarkdown) ReplaceMarkdown(_ context.Context, req *notes.ReplaceMarkdownRequest, _ ...grpc.CallOption) (*notes.ReplaceMarkdownResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if englishHash(m.files[req.Relpath]) != req.ExpectedRevision {
		return nil, status.Error(codes.Aborted, "stale")
	}
	m.files[req.Relpath] = req.Content
	return &notes.ReplaceMarkdownResponse{}, nil
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
