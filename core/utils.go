package core

import (
	"time"
)

func GetTodayFilename() string {
	logger.Debug("GetTodayFilename")
	cfg := GetConfig()
	return computeFilename(time.Now().UTC(), cfg.TimezoneOffsetHours, cfg.DayStartHour)
}

func computeFilename(now time.Time, tzOffsetHours, dayStartHour int) string {
	localTime := now.Add(time.Duration(tzOffsetHours) * time.Hour)
	if localTime.Hour() < dayStartHour {
		localTime = localTime.AddDate(0, 0, -1)
	}
	return localTime.Format("02-Jan-2006") + ".md"
}
