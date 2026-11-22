package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/notes"
)

func TestMarkdownReplacementPreservesRawContentAndRejectsStaleWrites(t *testing.T) {
	vault := setupRealConfig(t)
	server := &NotesServer{}
	ctx := t.Context()
	const file = "Example/document.md"
	get := func() *pb.ReadMarkdownResponse {
		result, err := server.ReadMarkdown(ctx, &pb.ReadMarkdownRequest{Relpath: file})
		require.NoError(t, err)
		return result
	}
	missing := get()
	require.Equal(t, missingMarkdownRevision, missing.Revision)
	text := "# Example\n<!-- keep metadata -->\n"
	_, err := server.ReplaceMarkdown(ctx, &pb.ReplaceMarkdownRequest{Relpath: file, ExpectedRevision: missing.Revision, Content: text})
	require.NoError(t, err)
	saved := get()
	require.Equal(t, text, saved.Content)
	require.NotEqual(t, missing.Revision, saved.Revision)
	_, err = server.ReplaceMarkdown(ctx, &pb.ReplaceMarkdownRequest{Relpath: file, ExpectedRevision: missing.Revision, Content: "stale"})
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Equal(t, text, get().Content)
	require.NoError(t, os.Symlink(filepath.Join(vault, file), filepath.Join(vault, "alias.md")))
	for _, invalid := range []string{"alias.md", "../other.md", ".obsidian/private.md", "secret.json"} {
		_, err := server.ReadMarkdown(ctx, &pb.ReadMarkdownRequest{Relpath: invalid})
		require.Error(t, err)
	}
}
