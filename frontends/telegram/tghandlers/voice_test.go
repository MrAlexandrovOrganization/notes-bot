package tghandlers

import (
	"context"
	"os"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/config"
)

func TestDownloadTelegramFile_LocalAPIReadsSharedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "voice-*.ogg")
	require.NoError(t, err)
	_, err = f.WriteString("audio bytes")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	a := &App{Cfg: &config.Config{LocalAPIURL: "http://telegram-bot-api:8081"}}
	rc, err := a.downloadTelegramFile(context.Background(), nil, tgbotapi.File{FilePath: f.Name()}, zap.NewNop())
	require.NoError(t, err)
	defer rc.Close()

	data, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	got := make([]byte, len(data))
	_, err = rc.Read(got)
	require.NoError(t, err)
	require.Equal(t, data, got)
}
