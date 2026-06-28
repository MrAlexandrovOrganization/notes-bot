// Package timeutil provides shared time utilities used across services.
package timeutil

import "time"

// FixedZone returns a timezone for the given UTC offset in hours.
func FixedZone(tzOffsetHours int) *time.Location {
	return time.FixedZone("local", tzOffsetHours*3600)
}

// LocalNow returns the current time in the specified timezone.
func LocalNow(tzOffsetHours int) time.Time {
	return time.Now().In(FixedZone(tzOffsetHours))
}

// TodayDate returns the current logical date as "DD-MMM-YYYY".
// The day boundary is at dayStartHour — before that hour, the previous calendar day is returned.
func TodayDate(tzOffsetHours, dayStartHour int) string {
	local := LocalNow(tzOffsetHours)
	if local.Hour() < dayStartHour {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("02-Jan-2006")
}

// LogicalToday returns the current logical date as a time.Time anchored at 00:00 local time.
// The day boundary is at dayStartHour — before that hour, the previous calendar day is returned.
// Используется LLM-промптами, чтобы "сегодня" и "завтра" совпадали с восприятием пользователя.
func LogicalToday(tzOffsetHours, dayStartHour int) time.Time {
	local := LocalNow(tzOffsetHours)
	if local.Hour() < dayStartHour {
		local = local.AddDate(0, 0, -1)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

// FormatLocalTime formats t in the given timezone as "DD.MM.YYYY HH:MM".
// Returns "" for zero time.
func FormatLocalTime(t time.Time, tzOffsetHours int) string {
	if t.IsZero() {
		return ""
	}
	return t.In(FixedZone(tzOffsetHours)).Format("02.01.2006 15:04")
}
