package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const templateContent = "---\n" +
	"date: \"[[{{date:DD-MMM-YYYY}}]]\"\n" +
	"title: \"[[{{date:DD-MMM-YYYY}}]]\"\n" +
	"tags:\n" +
	"  - daily\n" +
	"Оценка:\n" +
	"---\n" +
	"---\n\n"

// setupNotesEnv создаёт временную структуру директорий и подменяет конфиг
func setupNotesEnv(t *testing.T) (dailyDir string) {
	t.Helper()

	tmpDir := t.TempDir()
	dailyDir = filepath.Join(tmpDir, "Daily")
	templatesDir := filepath.Join(tmpDir, "Templates")
	require.NoError(t, os.MkdirAll(dailyDir, 0755))
	require.NoError(t, os.MkdirAll(templatesDir, 0755))

	templatePath := filepath.Join(templatesDir, "Daily.md")
	require.NoError(t, os.WriteFile(templatePath, []byte(templateContent), 0644))

	instance = &Config{
		NotesDir:            tmpDir,
		TemplateDir:         templatesDir,
		DailyNotesDir:       dailyDir,
		DailyTemplatePath:   templatePath,
		TimezoneOffsetHours: 3,
		DayStartHour:        7,
	}
	once = sync.Once{}
	once.Do(func() {})

	t.Cleanup(func() {
		instance = nil
		once = sync.Once{}
	})

	return dailyDir
}

// --- ReadNote ---

func TestReadNote_ReturnsContent(t *testing.T) {
	daily := setupNotesEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(daily, "01-Mar-2026.md"), []byte("hello world"), 0644))

	result, err := (&realNoteStore{}).ReadNote("01-Mar-2026")
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestReadNote_MissingFileReturnsEmpty(t *testing.T) {
	setupNotesEnv(t)

	result, err := (&realNoteStore{}).ReadNote("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestReadNote_UnicodeContent(t *testing.T) {
	daily := setupNotesEnv(t)
	content := "Привет мир\n- [ ] Задача"
	require.NoError(t, os.WriteFile(filepath.Join(daily, "01-Mar-2026.md"), []byte(content), 0644))

	result, err := (&realNoteStore{}).ReadNote("01-Mar-2026")
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

// --- EnsureNote (create from template) ---

func TestEnsureNote_CreatesFile(t *testing.T) {
	daily := setupNotesEnv(t)

	require.NoError(t, (&realNoteStore{}).EnsureNote("01-Mar-2026"))

	_, err := os.Stat(filepath.Join(daily, "01-Mar-2026.md"))
	assert.NoError(t, err)
}

func TestEnsureNote_SubstitutesDate(t *testing.T) {
	daily := setupNotesEnv(t)

	require.NoError(t, (&realNoteStore{}).EnsureNote("15-Apr-2026"))

	data, err := os.ReadFile(filepath.Join(daily, "15-Apr-2026.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "15-Apr-2026")
	assert.NotContains(t, string(data), "{{date:DD-MMM-YYYY}}")
}

func TestEnsureNote_FallbackWhenTemplateMissing(t *testing.T) {
	daily := setupNotesEnv(t)
	instance.DailyTemplatePath = filepath.Join(t.TempDir(), "NoSuch.md")

	require.NoError(t, (&realNoteStore{}).EnsureNote("01-Mar-2026"))

	_, err := os.Stat(filepath.Join(daily, "01-Mar-2026.md"))
	assert.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(daily, "01-Mar-2026.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "01-Mar-2026")
}

func TestEnsureNote_DoesNotOverwriteExisting(t *testing.T) {
	daily := setupNotesEnv(t)
	existing := "existing content"
	require.NoError(t, os.WriteFile(filepath.Join(daily, "01-Mar-2026.md"), []byte(existing), 0644))

	require.NoError(t, (&realNoteStore{}).EnsureNote("01-Mar-2026"))

	data, err := os.ReadFile(filepath.Join(daily, "01-Mar-2026.md"))
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

// --- AppendToNote ---

func TestAppendToNote_AppendsText(t *testing.T) {
	daily := setupNotesEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(daily, "01-Mar-2026.md"), []byte("line1\n"), 0644))

	require.NoError(t, (&realNoteStore{}).AppendToNote("01-Mar-2026", "line2"))

	data, err := os.ReadFile(filepath.Join(daily, "01-Mar-2026.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "line2")
}

func TestAppendToNote_CreatesFileIfMissing(t *testing.T) {
	daily := setupNotesEnv(t)

	require.NoError(t, (&realNoteStore{}).AppendToNote("01-Mar-2026", "new text"))

	_, err := os.Stat(filepath.Join(daily, "01-Mar-2026.md"))
	assert.NoError(t, err)
}
