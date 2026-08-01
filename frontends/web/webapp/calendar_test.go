package webapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCalendarWeeks_NovemberStartsOnSaturday(t *testing.T) {
	// 2025-11-01 is a Saturday — Monday-start week means 5 leading blanks.
	weeks := buildCalendarWeeks(2025, 11, "09-Nov-2025", map[string]bool{"09-Nov-2025": true})
	require.NotEmpty(t, weeks)

	first := weeks[0]
	require.Len(t, first, 7)
	for i := 0; i < 5; i++ {
		assert.Equal(t, 0, first[i].Day, "expected leading blank cell at index %d", i)
	}
	assert.Equal(t, 1, first[5].Day)
	assert.Equal(t, 2, first[6].Day)

	var found bool
	for _, week := range weeks {
		for _, day := range week {
			if day.Date == "09-Nov-2025" {
				found = true
				assert.True(t, day.HasNote)
				assert.True(t, day.IsToday)
			}
		}
	}
	assert.True(t, found, "expected to find 09-Nov-2025 in the calendar grid")

	total := 0
	for _, week := range weeks {
		for _, day := range week {
			if day.Day != 0 {
				total++
			}
		}
	}
	assert.Equal(t, 30, total, "November has 30 days")
}

func TestPrevNextMonth_WrapsYear(t *testing.T) {
	assert.Equal(t, 12, prevMonth(1))
	assert.Equal(t, 2025, prevMonthYear(2026, 1))
	assert.Equal(t, 1, nextMonth(12))
	assert.Equal(t, 2027, nextMonthYear(2026, 12))
}
