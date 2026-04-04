package core

import (
	"context"
	"fmt"
	"notes-bot/internal/telemetry"
	"os"
	"path/filepath"
	"sync"

	"go.risoftinc.com/goenv"
	"go.uber.org/zap"
)

const (
	notesDirEnv            = "NOTES_DIR"
	templateDirEnv         = "TEMPLATE_SUBDIR"
	timezoneOffsetHoursEnv = "TIMEZONE_OFFSET_HOURS"
	dayStartHourEnv        = "DAY_START_HOUR"
)

type Config struct {
	NotesDir            string
	TemplateDir         string
	DailyNotesDir       string
	DailyTemplatePath   string
	TimezoneOffsetHours int
	DayStartHour        int
}

var (
	instance *Config
	once     sync.Once
)

func GetConfig(ctx context.Context) *Config {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("GetConfig")

	once.Do(func() {
		notesDir := GetNotesDir(ctx)

		if err := ValidateDir(ctx, notesDir); err != nil {
			logger.Fatal("NOTES_DIR invalid", zap.Error(err))
		}

		templateSubdir := goenv.GetEnvString(templateDirEnv, "Templates")
		templateDir := filepath.Join(notesDir, templateSubdir)

		if err := ValidateDir(ctx, templateDir); err != nil {
			logger.Fatal("template directory invalid", zap.Error(err))
		}

		dailyNotesDir := filepath.Join(notesDir, "Daily")
		if err := os.MkdirAll(dailyNotesDir, 0755); err != nil {
			logger.Fatal("cannot create daily notes directory", zap.Error(err))
		}

		dailyTemplatePath := filepath.Join(templateDir, "Daily.md")
		if _, err := os.Stat(dailyTemplatePath); os.IsNotExist(err) {
			logger.Fatal("daily template not found", zap.String("path", dailyTemplatePath))
		}

		instance = &Config{
			NotesDir:            notesDir,
			TemplateDir:         templateDir,
			DailyNotesDir:       dailyNotesDir,
			DailyTemplatePath:   dailyTemplatePath,
			TimezoneOffsetHours: goenv.GetEnvInt(timezoneOffsetHoursEnv, 3),
			DayStartHour:        goenv.GetEnvInt(dayStartHourEnv, 7),
		}
	})
	return instance
}

func ValidateDir(ctx context.Context, path string) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("ValidateDir")

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}

func GetNotesDir(ctx context.Context) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("GetNotesDir")

	notesDir := goenv.GetEnvString(notesDirEnv, "")
	if notesDir == "" {
		logger.Fatal("NOTES_DIR environment variable must be set")
	}
	return notesDir
}
