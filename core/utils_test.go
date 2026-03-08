package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// computeFilename тестируется напрямую — не нужно мокать time.Now()

func utc(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
}

func TestComputeFilename_MiddayUTCReturnsToday(t *testing.T) {
	// 12:00 UTC → 15:00 Moscow → тот же день
	assert.Equal(t, "03-Mar-2026.md", computeFilename(utc(2026, 3, 3, 12, 0), 3, 7))
}

func TestComputeFilename_EveningUTCReturnsToday(t *testing.T) {
	// 20:00 UTC → 23:00 Moscow → тот же день
	assert.Equal(t, "03-Mar-2026.md", computeFilename(utc(2026, 3, 3, 20, 0), 3, 7))
}

func TestComputeFilename_EarlyMorningUTCReturnsPreviousDay(t *testing.T) {
	// 02:00 UTC → 05:00 Moscow → до 07:00 → предыдущий день
	assert.Equal(t, "02-Mar-2026.md", computeFilename(utc(2026, 3, 3, 2, 0), 3, 7))
}

func TestComputeFilename_ExactlyAtDayStartReturnsToday(t *testing.T) {
	// 04:00 UTC → 07:00 Moscow → ровно DAY_START_HOUR → сегодня
	assert.Equal(t, "03-Mar-2026.md", computeFilename(utc(2026, 3, 3, 4, 0), 3, 7))
}

func TestComputeFilename_OneMinuteBeforeDayStartReturnsPreviousDay(t *testing.T) {
	// 03:59 UTC → 06:59 Moscow → до 07:00 → предыдущий день
	assert.Equal(t, "02-Mar-2026.md", computeFilename(utc(2026, 3, 3, 3, 59), 3, 7))
}

func TestComputeFilename_MidnightCrossesMonthBoundary(t *testing.T) {
	// 01:00 UTC → 04:00 Moscow 1 марта → до 07:00 → 28 февраля
	assert.Equal(t, "28-Feb-2026.md", computeFilename(utc(2026, 3, 1, 1, 0), 3, 7))
}

func TestComputeFilename_HasMdExtension(t *testing.T) {
	result := computeFilename(utc(2026, 3, 3, 12, 0), 3, 7)
	assert.True(t, len(result) > 3 && result[len(result)-3:] == ".md")
}

func TestComputeFilename_Format(t *testing.T) {
	// Формат DD-Mon-YYYY.md
	assert.Equal(t, "05-Jan-2026.md", computeFilename(utc(2026, 1, 5, 12, 0), 3, 7))
}

// --- GetTodayFilename ---

func TestGetTodayFilename_ReturnsValidFormat(t *testing.T) {
	setupNotesEnv(t) // инициализирует конфиг с нужными timezone-параметрами
	result := GetTodayFilename()
	assert.Regexp(t, `^\d{2}-[A-Z][a-z]{2}-\d{4}\.md$`, result)
}

func TestGetTodayFilename_HasMdExtension(t *testing.T) {
	setupNotesEnv(t)
	result := GetTodayFilename()
	assert.Equal(t, ".md", result[len(result)-3:])
}
