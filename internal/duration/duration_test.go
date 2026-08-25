package duration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── parseDuration ──────────────────────────────────────────────────────────

func TestParseDuration_BareInteger(t *testing.T) {
	n, err := Parse("30")
	assert.NoError(t, err)
	assert.Equal(t, 30, n)
}

func TestParseDuration_BareIntegerWithSpaces(t *testing.T) {
	n, err := Parse("  120  ")
	assert.NoError(t, err)
	assert.Equal(t, 120, n)
}

func TestParseDuration_BareIntegerZero(t *testing.T) {
	_, err := Parse("0")
	assert.Error(t, err)
}

func TestParseDuration_BareIntegerNegative(t *testing.T) {
	_, err := Parse("-10")
	assert.Error(t, err)
}

func TestParseDuration_Minutes(t *testing.T) {
	n, err := Parse("30m")
	assert.NoError(t, err)
	assert.Equal(t, 30, n)
}

func TestParseDuration_Hours(t *testing.T) {
	n, err := Parse("2h")
	assert.NoError(t, err)
	assert.Equal(t, 120, n)
}

func TestParseDuration_Days(t *testing.T) {
	n, err := Parse("1d")
	assert.NoError(t, err)
	assert.Equal(t, 1440, n)
}

func TestParseDuration_Weeks(t *testing.T) {
	n, err := Parse("1w")
	assert.NoError(t, err)
	assert.Equal(t, 10080, n)
}

func TestParseDuration_Months(t *testing.T) {
	n, err := Parse("1M")
	assert.NoError(t, err)
	assert.Equal(t, 43200, n)
}

func TestParseDuration_HoursAndMinutes(t *testing.T) {
	n, err := Parse("1h30m")
	assert.NoError(t, err)
	assert.Equal(t, 90, n)
}

func TestParseDuration_DaysHoursMinutes(t *testing.T) {
	n, err := Parse("1d3h33m")
	assert.NoError(t, err)
	assert.Equal(t, 1440+3*60+33, n)
}

func TestParseDuration_DaysHoursMinutesWithSpaces(t *testing.T) {
	n, err := Parse("1d 3h 33m")
	assert.NoError(t, err)
	assert.Equal(t, 1440+3*60+33, n)
}

func TestParseDuration_WeeksAndDays(t *testing.T) {
	n, err := Parse("2w3d")
	assert.NoError(t, err)
	assert.Equal(t, 2*10080+3*1440, n)
}

func TestParseDuration_MonthsAndDays(t *testing.T) {
	n, err := Parse("1M2d")
	assert.NoError(t, err)
	assert.Equal(t, 43200+2*1440, n)
}

func TestParseDuration_OverflowMinutes(t *testing.T) {
	_, err := Parse("27h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1d3h")
}

func TestParseDuration_OverflowMinutesExact(t *testing.T) {
	_, err := Parse("24h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1d")
}

func TestParseDuration_OverflowSeconds(t *testing.T) {
	_, err := Parse("65m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1h5m")
}

func TestParseDuration_OverflowMinutesExactHour(t *testing.T) {
	_, err := Parse("60m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1h")
}

func TestParseDuration_OverflowDays(t *testing.T) {
	_, err := Parse("8d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1w1d")
}

func TestParseDuration_OverflowDaysExactWeek(t *testing.T) {
	_, err := Parse("7d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1w")
}

func TestParseDuration_DuplicateUnit(t *testing.T) {
	_, err := Parse("1h2h")
	assert.Error(t, err)
}

func TestParseDuration_UnknownUnit(t *testing.T) {
	_, err := Parse("5y")
	assert.Error(t, err)
}

func TestParseDuration_Empty(t *testing.T) {
	_, err := Parse("")
	assert.Error(t, err)
}

func TestParseDuration_NoUnit(t *testing.T) {
	// "5" is a bare int (valid), "5 " also valid; "5abc" should fail
	_, err := Parse("5abc")
	assert.Error(t, err)
}

func TestParseDuration_NumberWithoutUnit(t *testing.T) {
	// trailing number without unit
	_, err := Parse("1h30")
	assert.Error(t, err)
}

// ── minutesToLabel ──────────────────────────────────────────────────────────

func TestMinutesToLabel_Minutes(t *testing.T) {
	assert.Equal(t, "30 мин.", MinutesToLabel(30))
}

func TestMinutesToLabel_ExactHour(t *testing.T) {
	assert.Equal(t, "1 ч.", MinutesToLabel(60))
}

func TestMinutesToLabel_HoursAndMinutes(t *testing.T) {
	assert.Equal(t, "1 ч. 30 мин.", MinutesToLabel(90))
}

func TestMinutesToLabel_ExactDay(t *testing.T) {
	assert.Equal(t, "1 д.", MinutesToLabel(1440))
}

func TestMinutesToLabel_DayAndHours(t *testing.T) {
	assert.Equal(t, "1 д. 3 ч.", MinutesToLabel(1440+3*60))
}

func TestMinutesToLabel_ExactWeek(t *testing.T) {
	assert.Equal(t, "1 нед.", MinutesToLabel(7*24*60))
}

func TestMinutesToLabel_WeekAndDays(t *testing.T) {
	assert.Equal(t, "1 нед. 2 д.", MinutesToLabel(7*24*60+2*24*60))
}

func TestMinutesToLabel_Month(t *testing.T) {
	assert.Equal(t, "1 мес.", MinutesToLabel(30*24*60))
}
