package tghandlers

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgstates"
)

// ── scheduleLabel ──────────────────────────────────────────────────────────

func TestScheduleLabel_KnownTypes(t *testing.T) {
	cases := map[string]string{
		"daily":       "каждый день",
		"weekly":      "по дням недели",
		"monthly":     "каждый месяц",
		"yearly":      "каждый год",
		"once":        "один раз",
		"custom_days": "каждые N дней",
	}
	for stype, want := range cases {
		assert.Equal(t, want, scheduleLabel(stype), "schedule type: %q", stype)
	}
}

func TestScheduleLabel_Unknown(t *testing.T) {
	assert.Equal(t, "unknown_type", scheduleLabel("unknown_type"))
}

// ── calPrompt ──────────────────────────────────────────────────────────────

func TestCalPrompt_Postpone(t *testing.T) {
	assert.Equal(t, tgfmt.Escape("📅 Выберите дату переноса:"), calPrompt("pp"))
}

func TestCalPrompt_Other(t *testing.T) {
	assert.Equal(t, tgfmt.Escape("📅 Выберите дату:"), calPrompt("once"))
	assert.Equal(t, tgfmt.Escape("📅 Выберите дату:"), calPrompt("yr"))
	assert.Equal(t, tgfmt.Escape("📅 Выберите дату:"), calPrompt(""))
}

// ── reminderListText ───────────────────────────────────────────────────────

func TestReminderListText_Empty(t *testing.T) {
	text := reminderListText(nil, 0, 0)
	assert.Contains(t, text.String(), "Напоминаний пока нет.")
}

func TestReminderListText_Header(t *testing.T) {
	text := reminderListText(nil, 0, 0)
	assert.True(t, strings.HasPrefix(text.String(), "🔔 Уведомления:"))
}

func TestReminderListText_SingleReminder(t *testing.T) {
	reminders := []*clients.ReminderInfo{
		{
			Title:        "Утренняя зарядка",
			ScheduleType: "daily",
			NextFireAt:   time.Date(2025, 1, 15, 9, 30, 0, 0, time.UTC),
		},
	}
	text := reminderListText(reminders, 0, 0)
	assert.Contains(t, text.String(), "Утренняя зарядка")
	assert.Contains(t, text.String(), "каждый день")
	assert.Contains(t, text.String(), "15.01.2025 09:30")
}

func TestReminderListText_PageClamping(t *testing.T) {
	// 3 reminders = 1 page. page=99 should be clamped to page 0.
	reminders := []*clients.ReminderInfo{
		{Title: "R1", ScheduleType: "daily"},
		{Title: "R2", ScheduleType: "weekly"},
		{Title: "R3", ScheduleType: "monthly"},
	}
	text := reminderListText(reminders, 99, 0)
	assert.Contains(t, text.String(), "R1")
	assert.Contains(t, text.String(), "R2")
	assert.Contains(t, text.String(), "R3")
}

func TestReminderListText_SecondPage(t *testing.T) {
	// 6 reminders: page 0 shows first 5, page 1 shows only the 6th ("Last").
	reminders := []*clients.ReminderInfo{
		{Title: "A", ScheduleType: "daily"},
		{Title: "B", ScheduleType: "daily"},
		{Title: "C", ScheduleType: "daily"},
		{Title: "D", ScheduleType: "daily"},
		{Title: "E", ScheduleType: "daily"},
		{Title: "Last", ScheduleType: "once"},
	}
	text := reminderListText(reminders, 1, 0)
	assert.Contains(t, text.String(), "Last")
	assert.NotContains(t, text.String(), "• A")
	assert.NotContains(t, text.String(), "• E")
}

// --- pluralDays ---

func TestPluralDays_One(t *testing.T) {
	assert.Equal(t, "день", pluralDays(1))
	assert.Equal(t, "день", pluralDays(21))
	assert.Equal(t, "день", pluralDays(31))
	assert.Equal(t, "день", pluralDays(101))
}

func TestPluralDays_Few(t *testing.T) {
	assert.Equal(t, "дня", pluralDays(2))
	assert.Equal(t, "дня", pluralDays(3))
	assert.Equal(t, "дня", pluralDays(4))
	assert.Equal(t, "дня", pluralDays(22))
	assert.Equal(t, "дня", pluralDays(23))
}

func TestPluralDays_Many(t *testing.T) {
	assert.Equal(t, "дней", pluralDays(5))
	assert.Equal(t, "дней", pluralDays(11)) // 11 — special case, not "день"
	assert.Equal(t, "дней", pluralDays(12)) // 12 — special case, not "дня"
	assert.Equal(t, "дней", pluralDays(20))
	assert.Equal(t, "дней", pluralDays(100))
}

// ── parseDuration ──────────────────────────────────────────────────────────

func TestParseDuration_BareInteger(t *testing.T) {
	n, err := parseDuration("30")
	assert.NoError(t, err)
	assert.Equal(t, 30, n)
}

func TestParseDuration_BareIntegerWithSpaces(t *testing.T) {
	n, err := parseDuration("  120  ")
	assert.NoError(t, err)
	assert.Equal(t, 120, n)
}

func TestParseDuration_BareIntegerZero(t *testing.T) {
	_, err := parseDuration("0")
	assert.Error(t, err)
}

func TestParseDuration_BareIntegerNegative(t *testing.T) {
	_, err := parseDuration("-10")
	assert.Error(t, err)
}

func TestParseDuration_Minutes(t *testing.T) {
	n, err := parseDuration("30m")
	assert.NoError(t, err)
	assert.Equal(t, 30, n)
}

func TestParseDuration_Hours(t *testing.T) {
	n, err := parseDuration("2h")
	assert.NoError(t, err)
	assert.Equal(t, 120, n)
}

func TestParseDuration_Days(t *testing.T) {
	n, err := parseDuration("1d")
	assert.NoError(t, err)
	assert.Equal(t, 1440, n)
}

func TestParseDuration_Weeks(t *testing.T) {
	n, err := parseDuration("1w")
	assert.NoError(t, err)
	assert.Equal(t, 10080, n)
}

func TestParseDuration_Months(t *testing.T) {
	n, err := parseDuration("1M")
	assert.NoError(t, err)
	assert.Equal(t, 43200, n)
}

func TestParseDuration_HoursAndMinutes(t *testing.T) {
	n, err := parseDuration("1h30m")
	assert.NoError(t, err)
	assert.Equal(t, 90, n)
}

func TestParseDuration_DaysHoursMinutes(t *testing.T) {
	n, err := parseDuration("1d3h33m")
	assert.NoError(t, err)
	assert.Equal(t, 1440+3*60+33, n)
}

func TestParseDuration_DaysHoursMinutesWithSpaces(t *testing.T) {
	n, err := parseDuration("1d 3h 33m")
	assert.NoError(t, err)
	assert.Equal(t, 1440+3*60+33, n)
}

func TestParseDuration_WeeksAndDays(t *testing.T) {
	n, err := parseDuration("2w3d")
	assert.NoError(t, err)
	assert.Equal(t, 2*10080+3*1440, n)
}

func TestParseDuration_MonthsAndDays(t *testing.T) {
	n, err := parseDuration("1M2d")
	assert.NoError(t, err)
	assert.Equal(t, 43200+2*1440, n)
}

func TestParseDuration_OverflowMinutes(t *testing.T) {
	_, err := parseDuration("27h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1d3h")
}

func TestParseDuration_OverflowMinutesExact(t *testing.T) {
	_, err := parseDuration("24h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1d")
}

func TestParseDuration_OverflowSeconds(t *testing.T) {
	_, err := parseDuration("65m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1h5m")
}

func TestParseDuration_OverflowMinutesExactHour(t *testing.T) {
	_, err := parseDuration("60m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1h")
}

func TestParseDuration_OverflowDays(t *testing.T) {
	_, err := parseDuration("8d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1w1d")
}

func TestParseDuration_OverflowDaysExactWeek(t *testing.T) {
	_, err := parseDuration("7d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1w")
}

func TestParseDuration_DuplicateUnit(t *testing.T) {
	_, err := parseDuration("1h2h")
	assert.Error(t, err)
}

func TestParseDuration_UnknownUnit(t *testing.T) {
	_, err := parseDuration("5y")
	assert.Error(t, err)
}

func TestParseDuration_Empty(t *testing.T) {
	_, err := parseDuration("")
	assert.Error(t, err)
}

func TestParseDuration_NoUnit(t *testing.T) {
	// "5" is a bare int (valid), "5 " also valid; "5abc" should fail
	_, err := parseDuration("5abc")
	assert.Error(t, err)
}

func TestParseDuration_NumberWithoutUnit(t *testing.T) {
	// trailing number without unit
	_, err := parseDuration("1h30")
	assert.Error(t, err)
}

// ── minutesToLabel ──────────────────────────────────────────────────────────

func TestMinutesToLabel_Minutes(t *testing.T) {
	assert.Equal(t, "30 мин.", minutesToLabel(30))
}

func TestMinutesToLabel_ExactHour(t *testing.T) {
	assert.Equal(t, "1 ч.", minutesToLabel(60))
}

func TestMinutesToLabel_HoursAndMinutes(t *testing.T) {
	assert.Equal(t, "1 ч. 30 мин.", minutesToLabel(90))
}

func TestMinutesToLabel_ExactDay(t *testing.T) {
	assert.Equal(t, "1 д.", minutesToLabel(1440))
}

func TestMinutesToLabel_DayAndHours(t *testing.T) {
	assert.Equal(t, "1 д. 3 ч.", minutesToLabel(1440+3*60))
}

func TestMinutesToLabel_ExactWeek(t *testing.T) {
	assert.Equal(t, "1 нед.", minutesToLabel(7*24*60))
}

func TestMinutesToLabel_WeekAndDays(t *testing.T) {
	assert.Equal(t, "1 нед. 2 д.", minutesToLabel(7*24*60+2*24*60))
}

func TestMinutesToLabel_Month(t *testing.T) {
	assert.Equal(t, "1 мес.", minutesToLabel(30*24*60))
}

// --- calMonthYear ---

func TestCalMonthYear_ZeroValues_UsesCurrentTime(t *testing.T) {
	uc := &tgstates.UserContext{} // month=0, year=0
	month, year := calMonthYear(uc, 3)
	assert.Greater(t, month, 0)
	assert.LessOrEqual(t, month, 12)
	assert.Greater(t, year, 2000)
}

func TestCalMonthYear_WithValues(t *testing.T) {
	uc := &tgstates.UserContext{ReminderCalMonth: 6, ReminderCalYear: 2026}
	month, year := calMonthYear(uc, 3)
	assert.Equal(t, 6, month)
	assert.Equal(t, 2026, year)
}
