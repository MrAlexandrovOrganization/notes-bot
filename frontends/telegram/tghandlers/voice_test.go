package tghandlers

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/config"
)

func TestDownloadTelegramFile_LocalAPIReadsSharedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "voice-*.oga")
	require.NoError(t, err)
	_, err = f.WriteString("audio bytes")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	a := &App{Cfg: &config.Config{LocalAPIURL: "http://telegram-bot-api:8081"}}
	rc, err := a.downloadTelegramFile(context.Background(), nil, telego.File{FilePath: f.Name()}, zap.NewNop())
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, []byte("audio bytes"), got)
}
