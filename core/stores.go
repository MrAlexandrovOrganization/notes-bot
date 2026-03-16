package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"notes_bot/core/features"

	"go.uber.org/zap"
)

// --- Interfaces ---

type CalendarStore interface {
	TodayDate() string
	GetExistingDates() ([]string, error)
}

type NoteStore interface {
	ReadNote(date string) (string, error)
	EnsureNote(date string) error
	AppendToNote(date, text string) error
}

type RatingStore interface {
	GetRating(content string) *int
	UpdateRating(date string, rating int) error
}

type TaskStore interface {
	ParseTasks(content string) []features.Task
	ToggleTask(date string, index int) error
	AddTask(date, text string) error
}

// --- realCalendarStore ---

type realCalendarStore struct{}

func (r *realCalendarStore) TodayDate() string {
	logger.Debug("TodayDate")
	return strings.TrimSuffix(GetTodayFilename(), ".md")
}

func (r *realCalendarStore) GetExistingDates() ([]string, error) {
	logger.Debug("GetExistingDates")
	dailyDir := GetConfig().DailyNotesDir
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
	sort.Strings(dates)
	return dates, nil
}

// --- realNoteStore ---

type realNoteStore struct{}

func (r *realNoteStore) ReadNote(date string) (string, error) {
	logger.Debug("ReadNote")
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	content, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	return string(content), nil
}

func (r *realNoteStore) EnsureNote(date string) error {
	logger.Debug("EnsureNote")
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return r.createFromTemplate(filePath, date)
	}
	return nil
}

func (r *realNoteStore) AppendToNote(date, text string) error {
	logger.Debug("AppendToNote")
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		if err := r.createFromTemplate(filePath, date); err != nil {
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

func (r *realNoteStore) createFromTemplate(filePath, dateStr string) error {
	logger.Debug("createFromTemplate")
	templatePath := GetConfig().DailyTemplatePath
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		zap.L().Warn("template not found, creating basic note")
		return r.createBasicNote(filePath, dateStr)
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

func (r *realNoteStore) createBasicNote(filePath, dateStr string) error {
	logger.Debug("createBasicNote")
	content := fmt.Sprintf("---\ndate: \"[[%s]]\"\ntitle: \"[[%s]]\"\nОценка:\ntags:\n  - daily\n---\n---\n", dateStr, dateStr)
	return os.WriteFile(filePath, []byte(content), 0644)
}

// --- realRatingStore ---

type realRatingStore struct{}

func (r *realRatingStore) GetRating(content string) *int {
	return features.GetRatingImpl(content)
}

func (r *realRatingStore) UpdateRating(date string, rating int) error {
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		zap.L().Error("file not found", zap.String("path", filePath))
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, ok := features.UpdateRatingImpl(string(data), rating)
	if ok {
		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			return err
		}
		zap.L().Info("successfully updated rating", zap.Int("rating", rating), zap.String("path", filePath))
	} else {
		zap.L().Warn("failed to update rating", zap.Int("rating", rating), zap.String("path", filePath))
	}
	return nil
}

// --- realTaskStore ---

type realTaskStore struct{}

func (r *realTaskStore) ParseTasks(content string) []features.Task {
	logger.Debug("ParseTasks")
	return features.ParseTasks(content)
}

func (r *realTaskStore) ToggleTask(date string, index int) error {
	logger.Debug("ToggleTask")
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.ToggleTaskContent(string(data), index)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(newContent), 0644)
}

func (r *realTaskStore) AddTask(date, text string) error {
	logger.Debug("AddTask")
	filePath := filepath.Join(GetConfig().DailyNotesDir, date+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	newContent, err := features.AddTaskContent(string(data), text)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(newContent), 0644)
}
