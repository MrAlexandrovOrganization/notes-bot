package core

import (
	"context"
	"time"

	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

func GetTodayFilename(ctx context.Context) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("GetTodayFilename")
	cfg := GetConfig(ctx)
	return timeutil.TodayDateAt(time.Now().UTC(), cfg.TimezoneOffsetHours, cfg.DayStartHour) + ".md"
}
