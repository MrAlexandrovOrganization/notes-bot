package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"notes-bot/core/features"
	"notes-bot/internal/telemetry"

	"go.uber.org/zap"
)

// --- Interfaces ---

type CalendarStore interface {
	TodayDate(ctx context.Context) string
	GetExistingDates(ctx context.Context) ([]string, error)
}

type NoteStore interface {
	ReadNote(ctx context.Context, date string) (string, error)
	EnsureNote(ctx context.Context, date string) error
	AppendToNote(ctx context.Context, date, text string) error
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

func (r *realNoteStore) ReadNote(ctx context.Context, date string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ReadNote")
	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
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
	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return r.createFromTemplate(ctx, filePath, date)
	}
	return nil
}

func (r *realNoteStore) AppendToNote(ctx context.Context, date, text string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("AppendToNote")
	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := r.createFromTemplate(ctx, filePath, date); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("error opening file for append: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", text)
	return err
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
	if err := os.WriteFile(filePath, []byte(result), 0644); err != nil {
		zap.L().Error("error writing note", zap.Error(err))
		return fmt.Errorf("error writing note: %w", err)
	}
	zap.L().Info("created daily note from template")
	return nil
}

func (r *realNoteStore) createBasicNote(ctx context.Context, filePath, dateStr string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("createBasicNote")
	content := fmt.Sprintf("---\ndate: \"[[%s]]\"\ntitle: \"[[%s]]\"\nОценка:\ntags:\n  - daily\n---\n---\n", dateStr, dateStr)
	return os.WriteFile(filePath, []byte(content), 0644)
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

	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
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
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
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
	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.ToggleTaskContent(ctx, string(data), index)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(newContent), 0644)
}

func (r *realTaskStore) AddTask(ctx context.Context, date, text string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("AddTask")
	filePath := filepath.Join(GetConfig(ctx).DailyNotesDir, date+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.AddTaskContent(ctx, string(data), text)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(newContent), 0644)
}
