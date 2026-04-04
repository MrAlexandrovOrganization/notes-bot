package core

import (
	"context"
	"notes-bot/internal/telemetry"
	"time"
)

func GetTodayFilename(ctx context.Context) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("GetTodayFilename")
	cfg := GetConfig(ctx)
	return computeFilename(ctx, time.Now().UTC(), cfg.TimezoneOffsetHours, cfg.DayStartHour)
}

func computeFilename(ctx context.Context, now time.Time, tzOffsetHours, dayStartHour int) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	logger.Debug("computeFilename")
	localTime := now.Add(time.Duration(tzOffsetHours) * time.Hour)
	if localTime.Hour() < dayStartHour {
		localTime = localTime.AddDate(0, 0, -1)
	}
	return localTime.Format("02-Jan-2006") + ".md"
}
