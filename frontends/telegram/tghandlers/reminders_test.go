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
