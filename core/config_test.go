package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRealConfig создаёт реальное окружение для тестирования GetConfig через sync.Once
func setupRealConfig(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	templatesDir := filepath.Join(tmpDir, "Templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "Daily.md"), []byte("template"), 0644))

	t.Setenv("NOTES_DIR", tmpDir)

	// Сбрасываем singleton перед тестом и восстанавливаем после
	instance = nil
	once = sync.Once{}
	t.Cleanup(func() {
		instance = nil
		once = sync.Once{}
	})

	return tmpDir
}

// --- ValidateDir ---

func TestValidateDir_ValidDirectory(t *testing.T) {
	assert.NoError(t, ValidateDir(context.Background(), t.TempDir()))
}

func TestValidateDir_NonexistentPath(t *testing.T) {
	err := ValidateDir(context.Background(), filepath.Join(t.TempDir(), "nonexistent"))
	assert.Error(t, err)
}

func TestValidateDir_FileIsNotDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))
	assert.Error(t, ValidateDir(context.Background(), path))
}

// --- GetNotesDir ---

func TestGetNotesDir_ReturnsEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOTES_DIR", dir)
	assert.Equal(t, dir, GetNotesDir(context.Background()))
}

// --- GetConfig ---

func TestGetConfig_NotesDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	assert.Equal(t, tmpDir, GetConfig(context.Background()).NotesDir)
}

func TestGetConfig_CreatesDailyDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	GetConfig(context.Background())
	_, err := os.Stat(filepath.Join(tmpDir, "Daily"))
	assert.NoError(t, err)
}

func TestGetConfig_DailyNotesDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	assert.Equal(t, filepath.Join(tmpDir, "Daily"), GetConfig(context.Background()).DailyNotesDir)
}

func TestGetConfig_DailyTemplatePath(t *testing.T) {
	tmpDir := setupRealConfig(t)
	expected := filepath.Join(tmpDir, "Templates", "Daily.md")
	assert.Equal(t, expected, GetConfig(context.Background()).DailyTemplatePath)
}

func TestGetConfig_DefaultTimezoneAndDayStart(t *testing.T) {
	setupRealConfig(t)
	cfg := GetConfig(context.Background())
	assert.Equal(t, 3, cfg.TimezoneOffsetHours)
	assert.Equal(t, 7, cfg.DayStartHour)
}

func TestGetConfig_Singleton(t *testing.T) {
	setupRealConfig(t)
	cfg1 := GetConfig(context.Background())
	cfg2 := GetConfig(context.Background())
	assert.Same(t, cfg1, cfg2)
}
