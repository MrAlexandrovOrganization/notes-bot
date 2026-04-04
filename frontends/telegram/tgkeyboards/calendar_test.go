package tgkeyboards

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── MonthName ──────────────────────────────────────────────────────────────

func TestMonthName_AllMonths(t *testing.T) {
	cases := map[int]string{
		1: "Январь", 2: "Февраль", 3: "Март", 4: "Апрель",
		5: "Май", 6: "Июнь", 7: "Июль", 8: "Август",
		9: "Сентябрь", 10: "Октябрь", 11: "Ноябрь", 12: "Декабрь",
	}
	for month, want := range cases {
		assert.Equal(t, want, MonthName(month), "month %d", month)
	}
}

func TestMonthName_Invalid(t *testing.T) {
	// Out-of-range returns empty string (zero value from map lookup).
	assert.Equal(t, "", MonthName(0))
	assert.Equal(t, "", MonthName(13))
}

// ── Calendar ───────────────────────────────────────────────────────────────

// January 2025: starts on Wednesday (offset=2), 31 days.
func TestCalendar_Structure_January2025(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 1, "", nil)
	rows := kb.InlineKeyboard
	// Min rows: header + weekdays + day rows + footer.
	require.GreaterOrEqual(t, len(rows), 4)
}

func TestCalendar_HeaderRow(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 3, "", nil)
	header := kb.InlineKeyboard[0]
	require.Len(t, header, 3)
	assert.Equal(t, "◀", header[0].Text)
	assert.Equal(t, "▶", header[2].Text)
	assert.Contains(t, header[1].Text, "Март")
	assert.Contains(t, header[1].Text, "2025")
}

func TestCalendar_WeekdayRow(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 1, "", nil)
	weekdays := kb.InlineKeyboard[1]
	require.Len(t, weekdays, 7)
	assert.Equal(t, "Пн", weekdays[0].Text)
	assert.Equal(t, "Вс", weekdays[6].Text)
}

func TestCalendar_FooterButtons(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 1, "", nil)
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	texts := make([]string, len(lastRow))
	for i, btn := range lastRow {
		texts[i] = btn.Text
	}
	assert.Contains(t, texts, "📅 Сегодня")
	assert.Contains(t, texts, "◀ Назад")
}

func TestCalendar_ActiveDateMarked(t *testing.T) {
	// January 2025, mark day 15 as active.
	activeDate := "15-Jan-2025"
	kb := Calendar(t.Context(), 2025, 1, activeDate, nil)
	found := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.Text == "[15]" {
				found = true
			}
		}
	}
	assert.True(t, found, "active date should be marked with [N]")
}

func TestCalendar_ExistingDateMarked(t *testing.T) {
	existingDates := map[string]bool{"10-Jan-2025": true}
	kb := Calendar(t.Context(), 2025, 1, "", existingDates)
	found := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.Text == "*10*" {
				found = true
			}
		}
	}
	assert.True(t, found, "existing date should be marked with *N*")
}

func TestCalendar_DayCallbackFormat(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 1, "", nil)
	for _, row := range kb.InlineKeyboard[2 : len(kb.InlineKeyboard)-1] {
		for _, btn := range row {
			if btn.Text == " " {
				continue
			}
			// Day buttons should have callback "cal:select:DD-MMM-YYYY".
			cb := ""
			if btn.CallbackData != nil {
				cb = *btn.CallbackData
			}
			assert.True(t,
				strings.HasPrefix(cb, "cal:select:"),
				"unexpected callback: %s", cb,
			)
		}
	}
}

func TestCalendar_AllDaysPresent(t *testing.T) {
	// February 2025 has 28 days.
	kb := Calendar(t.Context(), 2025, 2, "", nil)
	dayCount := 0
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "cal:select:") {
				dayCount++
			}
		}
	}
	assert.Equal(t, 28, dayCount)
}

func TestCalendar_NavigationCallbacks(t *testing.T) {
	kb := Calendar(t.Context(), 2025, 5, "", nil)
	header := kb.InlineKeyboard[0]
	require.NotNil(t, header[0].CallbackData)
	require.NotNil(t, header[2].CallbackData)
	assert.Equal(t, "cal:prev", *header[0].CallbackData)
	assert.Equal(t, "cal:next", *header[2].CallbackData)
}

func TestCalendar_HeaderContainsMonthYear(t *testing.T) {
	kb := Calendar(t.Context(), 2026, 11, "", nil)
	header := kb.InlineKeyboard[0]
	assert.Contains(t, header[1].Text, "Ноябрь")
	assert.Contains(t, header[1].Text, fmt.Sprintf("%d", 2026))
}
