package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"notes-bot/core/features"
	"notes-bot/internal/telemetry"

	"go.uber.org/zap"
)

// --- File locking & atomic writes ---
//
// Telegram and web frontends hit the same core concurrently; every
// read-modify-write on a note file must hold a per-file lock, otherwise
// updates are lost. Writes go through temp-file+rename so a crash mid-write
// cannot corrupt a note.

var fileLocks sync.Map // map[abs path]*sync.Mutex

// lockFile acquires the per-file mutex for path and returns the unlock func.
func lockFile(path string) func() {
	muAny, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// writeFileAtomic writes data to filePath via a temp file in the same
// directory followed by an atomic rename.
func writeFileAtomic(filePath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName) //nolint:errcheck
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	cleanup = false
	return nil
}

// --- Interfaces ---

type CalendarStore interface {
	TodayDate(ctx context.Context) string
	GetExistingDates(ctx context.Context) ([]string, error)
}

type DirEntry struct {
	Name    string
	Relpath string
	IsDir   bool
}

type NoteStore interface {
	ReadNote(ctx context.Context, date string) (string, error)
	EnsureNote(ctx context.Context, date string) error
	AppendToNote(ctx context.Context, date, text string) error
	AppendByPath(ctx context.Context, relpath, text string) error
	ListDirectory(ctx context.Context, relpath string) ([]DirEntry, error)
	ReadNoteByPath(ctx context.Context, relpath string) (string, error)
}

type RatingStore interface {
	GetRating(ctx context.Context, content string) *int
	UpdateRating(ctx context.Context, date string, rating int) error
}

type TaskStore interface {
	ParseTasks(ctx context.Context, content string) []features.Task
	ToggleTask(ctx context.Context, date string, index int) error
	AddTask(ctx context.Context, date, text string) error
}

// --- realCalendarStore ---

type realCalendarStore struct{}

func (r *realCalendarStore) TodayDate(ctx context.Context) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("TodayDate")
	return strings.TrimSuffix(GetTodayFilename(ctx), ".md")
}

func (r *realCalendarStore) GetExistingDates(ctx context.Context) ([]string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("GetExistingDates")
	dailyDir := GetConfig(ctx).DailyNotesDir
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		return nil, fmt.Errorf("error reading daily notes dir: %w", err)
	}

	var dates []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			dates = append(dates, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	slices.Sort(dates)
	return dates, nil
}

// --- realNoteStore ---

type realNoteStore struct{}

func dailyNotePath(ctx context.Context, date string) (string, error) {
	return resolveVaultPath(GetConfig(ctx).DailyNotesDir, date+".md")
}

func (r *realNoteStore) ReadNote(ctx context.Context, date string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ReadNote")
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	return string(content), nil
}

func (r *realNoteStore) EnsureNote(ctx context.Context, date string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("EnsureNote")
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock := lockFile(filePath)
	defer unlock()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return r.createFromTemplate(ctx, filePath, date)
	}
	return nil
}

func (r *realNoteStore) AppendToNote(ctx context.Context, date, text string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("AppendToNote")
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock := lockFile(filePath)
	defer unlock()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := r.createFromTemplate(ctx, filePath, date); err != nil {
			return err
		}
	}
	return appendToFile(filePath, text)
}

func (r *realNoteStore) AppendByPath(ctx context.Context, relpath, text string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("AppendByPath")
	notesDir := GetConfig(ctx).NotesDir
	filePath, err := resolveVaultPath(notesDir, relpath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("note not found: %s", relpath)
		}
		return fmt.Errorf("stat note: %w", err)
	}
	unlock := lockFile(filePath)
	defer unlock()
	return appendToFile(filePath, text)
}

// resolveVaultPath joins notesDir + relpath and ensures the result stays under notesDir.
// Rejects absolute paths, parent traversal, and symlinks escaping the vault.
func resolveVaultPath(notesDir, relpath string) (string, error) {
	if relpath == "" {
		return "", fmt.Errorf("empty relpath")
	}
	if filepath.IsAbs(relpath) {
		return "", fmt.Errorf("relpath must be relative")
	}
	cleaned := filepath.Clean(relpath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("relpath escapes vault")
	}
	absVault, err := filepath.Abs(notesDir)
	if err != nil {
		return "", fmt.Errorf("abs vault: %w", err)
	}
	realVault, err := filepath.EvalSymlinks(absVault)
	if err != nil {
		return "", fmt.Errorf("resolve vault: %w", err)
	}
	full := filepath.Join(absVault, cleaned)
	if !strings.HasPrefix(full, absVault+string(filepath.Separator)) && full != absVault {
		return "", fmt.Errorf("relpath escapes vault")
	}
	// Resolve every existing ancestor to catch symlinks while still allowing a
	// not-yet-existing leaf (EnsureNote creates new daily notes).
	existing := full
	var suffix []string
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat path: %w", statErr)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("cannot find existing path ancestor")
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	realFull, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		realFull = filepath.Join(realFull, suffix[i])
	}
	if !strings.HasPrefix(realFull, realVault+string(filepath.Separator)) && realFull != realVault {
		return "", fmt.Errorf("relpath escapes vault through symlink")
	}
	return full, nil
}

func appendToFile(filePath, text string) error {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening file for append: %w", err)
	}
	defer f.Close()

	if needLeadingNewline(filePath) {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(f, "%s\n", text)
	return err
}

func needLeadingNewline(filePath string) bool {
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		return false
	}
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := f.Seek(-1, 2); err != nil {
		return false
	}
	last := make([]byte, 1)
	if _, err := f.Read(last); err != nil {
		return false
	}
	return last[0] != '\n'
}

func (r *realNoteStore) createFromTemplate(ctx context.Context, filePath, dateStr string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("createFromTemplate")
	templatePath := GetConfig(ctx).DailyTemplatePath
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		zap.L().Warn("template not found, creating basic note")
		return r.createBasicNote(ctx, filePath, dateStr)
	}
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("error reading template: %w", err)
	}
	result := strings.ReplaceAll(string(content), "{{date:DD-MMM-YYYY}}", dateStr)
	if err := writeFileAtomic(filePath, []byte(result), 0644); err != nil {
		zap.L().Error("error writing note", zap.Error(err))
		return fmt.Errorf("error writing note: %w", err)
	}
	zap.L().Info("created daily note from template")
	return nil
}

func (r *realNoteStore) createBasicNote(ctx context.Context, filePath, dateStr string) error {
	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("createBasicNote")
	content := fmt.Sprintf("---\ndate: \"[[%s]]\"\ntitle: \"[[%s]]\"\nОценка:\ntags:\n  - daily\n---\n---\n", dateStr, dateStr)
	return writeFileAtomic(filePath, []byte(content), 0644)
}

// --- realRatingStore ---

type realRatingStore struct{}

func (r *realRatingStore) GetRating(ctx context.Context, content string) *int {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	return features.GetRatingImpl(ctx, content)
}

func (r *realRatingStore) UpdateRating(ctx context.Context, date string, rating int) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock := lockFile(filePath)
	defer unlock()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		zap.L().Error("file not found", zap.String("path", filePath))
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, ok := features.UpdateRatingImpl(ctx, string(data), rating)
	if !ok {
		return fmt.Errorf("failed to update rating in file %s", filePath)
	}
	if err := writeFileAtomic(filePath, []byte(newContent), 0644); err != nil {
		return err
	}
	zap.L().Info("successfully updated rating", zap.Int("rating", rating), zap.String("path", filePath))
	return nil
}

// --- realTaskStore ---

type realTaskStore struct{}

func (r *realTaskStore) ParseTasks(ctx context.Context, content string) []features.Task {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ParseTasks")
	return features.ParseTasks(ctx, content)
}

func (r *realTaskStore) ToggleTask(ctx context.Context, date string, index int) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ToggleTask")
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock := lockFile(filePath)
	defer unlock()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.ToggleTaskContent(ctx, string(data), index)
	if err != nil {
		return err
	}
	return writeFileAtomic(filePath, []byte(newContent), 0644)
}

func (r *realTaskStore) AddTask(ctx context.Context, date, text string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("AddTask")
	filePath, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock := lockFile(filePath)
	defer unlock()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.AddTaskContent(ctx, string(data), text)
	if err != nil {
		return err
	}
	return writeFileAtomic(filePath, []byte(newContent), 0644)
}

func (r *realNoteStore) ListDirectory(ctx context.Context, relpath string) ([]DirEntry, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ListDirectory", zap.String("relpath", relpath))
	notesDir := GetConfig(ctx).NotesDir

	var dirPath string
	var err error
	if relpath == "" {
		dirPath = notesDir
	} else {
		dirPath, err = resolveVaultPath(notesDir, relpath)
		if err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("error reading directory: %w", err)
	}

	var result []DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		var entryRelpath string
		if relpath == "" {
			entryRelpath = name
		} else {
			entryRelpath = filepath.Join(relpath, name)
		}
		result = append(result, DirEntry{
			Name:    name,
			Relpath: entryRelpath,
			IsDir:   entry.IsDir(),
		})
	}

	slices.SortFunc(result, func(a, b DirEntry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return result, nil
}

func (r *realNoteStore) ReadNoteByPath(ctx context.Context, relpath string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ReadNoteByPath", zap.String("relpath", relpath))
	notesDir := GetConfig(ctx).NotesDir
	filePath, err := resolveVaultPath(notesDir, relpath)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	return string(content), nil
}
