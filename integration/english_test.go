package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"notes-bot/internal/english"
	pb "notes-bot/proto/english/v1"
)

func TestEnglishReadsAndWritesCanonicalMarkdownThroughCore(t *testing.T) {
	ctx := t.Context()
	server := &english.Server{Dictionary: english.Dictionary{Notes: client}}
	req := &pb.SaveCardRequest{OperationId: "integration-save", Card: &pb.Card{
		Kind: "word", Date: testDate, English: "cliff", Source: "утёс",
	}}
	saved, err := server.SaveCard(ctx, req)
	require.NoError(t, err)
	file := filepath.Join(tempDir, "Tags/English/words.md")
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Contains(t, string(raw), saved.Card.Id)
	require.NoError(t, os.WriteFile(file, []byte(strings.Replace(string(raw), "утёс", "обрыв", 1)), 0644))

	result, err := server.SaveCard(ctx, req)
	require.NoError(t, err)
	require.Equal(t, saved.Card.Id, result.Card.Id)
	require.Equal(t, "обрыв", result.Card.Source)
	listed, err := server.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Cards, 1)
	require.Equal(t, "обрыв", listed.Cards[0].Source)
}
