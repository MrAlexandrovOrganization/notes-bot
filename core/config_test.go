package core

import (
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
	assert.NoError(t, ValidateDir(t.Context(), t.TempDir()))
}

func TestValidateDir_NonexistentPath(t *testing.T) {
	err := ValidateDir(t.Context(), filepath.Join(t.TempDir(), "nonexistent"))
	assert.Error(t, err)
}

func TestValidateDir_FileIsNotDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))
	assert.Error(t, ValidateDir(t.Context(), path))
}

// --- GetNotesDir ---

func TestGetNotesDir_ReturnsEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NOTES_DIR", dir)
	assert.Equal(t, dir, GetNotesDir(t.Context()))
}

// --- GetConfig ---

func TestGetConfig_NotesDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	assert.Equal(t, tmpDir, GetConfig(t.Context()).NotesDir)
}

func TestGetConfig_CreatesDailyDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	GetConfig(t.Context())
	_, err := os.Stat(filepath.Join(tmpDir, "Daily"))
	assert.NoError(t, err)
}

func TestGetConfig_DailyNotesDir(t *testing.T) {
	tmpDir := setupRealConfig(t)
	assert.Equal(t, filepath.Join(tmpDir, "Daily"), GetConfig(t.Context()).DailyNotesDir)
}

func TestGetConfig_DailyTemplatePath(t *testing.T) {
	tmpDir := setupRealConfig(t)
	expected := filepath.Join(tmpDir, "Templates", "Daily.md")
	assert.Equal(t, expected, GetConfig(t.Context()).DailyTemplatePath)
}

func TestGetConfig_DefaultTimezoneAndDayStart(t *testing.T) {
	setupRealConfig(t)
	cfg := GetConfig(t.Context())
	assert.Equal(t, 3, cfg.TimezoneOffsetHours)
	assert.Equal(t, 7, cfg.DayStartHour)
}

func TestGetConfig_Singleton(t *testing.T) {
	setupRealConfig(t)
	cfg1 := GetConfig(t.Context())
	cfg2 := GetConfig(t.Context())
	assert.Same(t, cfg1, cfg2)
}
