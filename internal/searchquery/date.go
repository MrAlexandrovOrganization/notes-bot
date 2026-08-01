// Package searchquery extracts deterministic filters from natural-language
// note queries before semantic retrieval.
package searchquery

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DateRange struct {
	From string
	To   string
}

func (r DateRange) Empty() bool { return r.From == "" && r.To == "" }

var (
	explicitDateRE = regexp.MustCompile(`(?i)\b(?:\d{4}-\d{2}-\d{2}|\d{2}\.\d{2}\.\d{4}|\d{2}-[a-z]{3}-\d{4})\b`)
	lastDaysRE     = regexp.MustCompile(`(?i)(?:за|последние|за последние)\s+(\d{1,3})\s+(?:день|дня|дней)`)
	monthRE        = regexp.MustCompile(`(?i)(?:^|\s)(?:в|за)\s+(январ[ье]|феврал[ье]|март(?:е)?|апрел[ье]|ма[ей]|июн[ье]|июл[ье]|август(?:е)?|сентябр[ье]|октябр[ье]|ноябр[ье]|декабр[ье])(?:\s+(\d{4}))?`)
)

// ExtractDateRange recognizes the common temporal forms used by the notes UI.
// today must already reflect the user's timezone and logical day boundary.
func ExtractDateRange(query string, today time.Time) DateRange {
	today = day(today)
	lower := strings.ToLower(query)

	if matches := explicitDateRE.FindAllString(query, -1); len(matches) > 0 {
		var dates []time.Time
		for _, match := range matches {
			if parsed, ok := parseDate(match); ok {
				dates = append(dates, parsed)
			}
		}
		if len(dates) > 0 {
			from, to := dates[0], dates[0]
			for _, date := range dates[1:] {
				if date.Before(from) {
					from = date
				}
				if date.After(to) {
					to = date
				}
			}
			return dateRange(from, to)
		}
	}

	if strings.Contains(lower, "позавчера") {
		date := today.AddDate(0, 0, -2)
		return dateRange(date, date)
	}
	if strings.Contains(lower, "вчера") {
		date := today.AddDate(0, 0, -1)
		return dateRange(date, date)
	}
	if strings.Contains(lower, "сегодня") {
		return dateRange(today, today)
	}

	if strings.Contains(lower, "на прошлой неделе") || strings.Contains(lower, "за прошлую неделю") {
		thisMonday := today.AddDate(0, 0, -weekdayOffset(today))
		return dateRange(thisMonday.AddDate(0, 0, -7), thisMonday.AddDate(0, 0, -1))
	}
	if strings.Contains(lower, "на этой неделе") || strings.Contains(lower, "за эту неделю") {
		monday := today.AddDate(0, 0, -weekdayOffset(today))
		return dateRange(monday, today)
	}
	if strings.Contains(lower, "за последнюю неделю") || strings.Contains(lower, "за неделю") {
		return dateRange(today.AddDate(0, 0, -6), today)
	}

	if strings.Contains(lower, "в прошлом месяце") || strings.Contains(lower, "за прошлый месяц") {
		thisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		lastMonth := thisMonth.AddDate(0, -1, 0)
		return dateRange(lastMonth, thisMonth.AddDate(0, 0, -1))
	}
	if strings.Contains(lower, "в этом месяце") || strings.Contains(lower, "за этот месяц") {
		start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		return dateRange(start, today)
	}

	if match := lastDaysRE.FindStringSubmatch(lower); len(match) == 2 {
		if count, err := strconv.Atoi(match[1]); err == nil && count > 0 {
			return dateRange(today.AddDate(0, 0, -(count-1)), today)
		}
	}

	if match := monthRE.FindStringSubmatch(lower); len(match) >= 2 {
		month, ok := russianMonth(match[1])
		if ok {
			year := today.Year()
			if len(match) >= 3 && match[2] != "" {
				year, _ = strconv.Atoi(match[2])
			} else if month > today.Month() {
				year--
			}
			start := time.Date(year, month, 1, 0, 0, 0, 0, today.Location())
			end := start.AddDate(0, 1, -1)
			return dateRange(start, end)
		}
	}

	return DateRange{}
}

func parseDate(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02", "02.01.2006", "02-Jan-2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return day(parsed), true
		}
	}
	return time.Time{}, false
}

func dateRange(from, to time.Time) DateRange {
	return DateRange{From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
}

func day(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func weekdayOffset(value time.Time) int {
	return (int(value.Weekday()) + 6) % 7
}

func russianMonth(value string) (time.Month, bool) {
	switch {
	case strings.HasPrefix(value, "январ"):
		return time.January, true
	case strings.HasPrefix(value, "феврал"):
		return time.February, true
	case strings.HasPrefix(value, "март"):
		return time.March, true
	case strings.HasPrefix(value, "апрел"):
		return time.April, true
	case strings.HasPrefix(value, "ма"):
		return time.May, true
	case strings.HasPrefix(value, "июн"):
		return time.June, true
	case strings.HasPrefix(value, "июл"):
		return time.July, true
	case strings.HasPrefix(value, "август"):
		return time.August, true
	case strings.HasPrefix(value, "сентябр"):
		return time.September, true
	case strings.HasPrefix(value, "октябр"):
		return time.October, true
	case strings.HasPrefix(value, "ноябр"):
		return time.November, true
	case strings.HasPrefix(value, "декабр"):
		return time.December, true
	default:
		return 0, false
	}
}
