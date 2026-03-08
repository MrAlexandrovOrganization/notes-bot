// Package timeutil provides shared time utilities used across services.
package timeutil

import (
	"strings"
	"time"
)

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

// FormatLocalTime parses an RFC3339 UTC timestamp and returns it formatted in the local timezone.
// Returns "—" for empty input; falls back to a truncated raw string on parse error.
func FormatLocalTime(utcStr string, tzOffsetHours int) string {
	if utcStr == "" {
		return "—"
	}
	s := strings.ReplaceAll(utcStr, "Z", "+00:00")
	dt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if len(utcStr) >= 16 {
			return utcStr[:16]
		}
		return utcStr
	}
	return dt.In(FixedZone(tzOffsetHours)).Format("02.01.2006 15:04")
}
