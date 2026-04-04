package tgkeyboards

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notes-bot/frontends/telegram/clients"
)

// --- MainMenu ---

func TestMainMenu_ContainsExpectedCallbacks(t *testing.T) {
	kb := MainMenu("")
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	for _, expected := range []string{"menu:rating", "menu:tasks", "menu:calendar", "menu:notifications"} {
		assert.Contains(t, cbs, expected)
	}
}

func TestMainMenu_IgnoresArgument(t *testing.T) {
	kb1 := MainMenu("")
	kb2 := MainMenu("anything")
	assert.Equal(t, len(kb1.InlineKeyboard), len(kb2.InlineKeyboard))
}

// --- ReminderNotification ---

func TestReminderNotification_WithTask(t *testing.T) {
	kb := ReminderNotification(42, true, "01-Mar-2026")
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, cbs, "reminder:done:42:1:01-Mar-2026")
	assert.Contains(t, cbs, "reminder:postpone_hours:1:42")
	assert.Contains(t, cbs, "reminder:postpone_hours:3:42")
	assert.Contains(t, cbs, "reminder:postpone:1:42")
	assert.Contains(t, cbs, "reminder:postpone:3:42")
	assert.Contains(t, cbs, "reminder:custom_date:42")
}

func TestReminderNotification_WithoutTask(t *testing.T) {
	kb := ReminderNotification(42, false, "")
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, cbs, "reminder:done:42:0")
	for _, cb := range cbs {
		assert.False(t, strings.HasPrefix(cb, "reminder:done:42:1:"), "should not have task done callback")
	}
}

func TestReminderNotification_CreateTaskFalseEmptyDate(t *testing.T) {
	// createTask=true but todayDate="" → falls back to "reminder:done:ID:0"
	kb := ReminderNotification(7, true, "")
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, cbs, "reminder:done:7:0")
}

// --- RemindersList ---

func TestRemindersList_Empty(t *testing.T) {
	kb := RemindersList(nil, 0)
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, cbs, "reminder:create")
	assert.Contains(t, cbs, "reminder:create_nl")
	assert.Contains(t, cbs, "reminder:back")
}

func TestRemindersList_WithReminders(t *testing.T) {
	reminders := []*clients.ReminderInfo{
		{ID: 1, Title: "Morning standup"},
		{ID: 2, Title: "Evening review"},
	}
	kb := RemindersList(reminders, 0)
	var deleteCbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "reminder:delete:") {
				deleteCbs = append(deleteCbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, deleteCbs, "reminder:delete:1")
	assert.Contains(t, deleteCbs, "reminder:delete:2")
}

func TestRemindersList_LongTitleTruncated(t *testing.T) {
	longTitle := strings.Repeat("X", 50)
	kb := RemindersList([]*clients.ReminderInfo{{ID: 1, Title: longTitle}}, 0)
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.Contains(btn.Text, "🔔") {
				label := strings.TrimPrefix(btn.Text, "🔔 ")
				assert.LessOrEqual(t, len(label), 30)
			}
		}
	}
}

func TestRemindersList_Pagination(t *testing.T) {
	reminders := make([]*clients.ReminderInfo, 8)
	for i := range reminders {
		reminders[i] = &clients.ReminderInfo{ID: int64(i + 1), Title: fmt.Sprintf("R%d", i+1)}
	}

	kb0 := RemindersList(reminders, 0)
	var nextCbs []string
	for _, row := range kb0.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "reminder:page:") {
				nextCbs = append(nextCbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, nextCbs, "reminder:page:1", "page 0 should have next-page button")
}

func TestRemindersList_SecondPage(t *testing.T) {
	reminders := make([]*clients.ReminderInfo, 8)
	for i := range reminders {
		reminders[i] = &clients.ReminderInfo{ID: int64(i + 1), Title: fmt.Sprintf("R%d", i+1)}
	}

	kb1 := RemindersList(reminders, 1)
	var prevCbs []string
	for _, row := range kb1.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "reminder:page:") {
				prevCbs = append(prevCbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, prevCbs, "reminder:page:0", "page 1 should have prev-page button")
}

// --- NLReminderConfirm ---

func TestNLReminderConfirm_Buttons(t *testing.T) {
	kb := NLReminderConfirm()
	require.Len(t, kb.InlineKeyboard, 1)
	require.Len(t, kb.InlineKeyboard[0], 3)
	var cbs []string
	for _, btn := range kb.InlineKeyboard[0] {
		if btn.CallbackData != nil {
			cbs = append(cbs, *btn.CallbackData)
		}
	}
	assert.Contains(t, cbs, "reminder:nl_confirm")
	assert.Contains(t, cbs, "reminder:create")
	assert.Contains(t, cbs, "reminder:cancel")
}

// --- TaskConfirm ---

func TestTaskConfirm_Buttons(t *testing.T) {
	kb := TaskConfirm()
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, cbs, "reminder:task_confirm:yes")
	assert.Contains(t, cbs, "reminder:task_confirm:no")
	assert.Contains(t, cbs, "reminder:cancel")
}

// --- ScheduleType ---

func TestScheduleType_HasAllTypes(t *testing.T) {
	kb := ScheduleType()
	var cbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				cbs = append(cbs, *btn.CallbackData)
			}
		}
	}
	for _, stype := range []string{"daily", "weekly", "monthly", "yearly", "once", "custom_days"} {
		assert.Contains(t, cbs, fmt.Sprintf("reminder:type:%s", stype))
	}
	assert.Contains(t, cbs, "reminder:cancel")
}

// --- ReminderCancel ---

func TestReminderCancel_SingleButton(t *testing.T) {
	kb := ReminderCancel()
	require.Len(t, kb.InlineKeyboard, 1)
	require.Len(t, kb.InlineKeyboard[0], 1)
	require.NotNil(t, kb.InlineKeyboard[0][0].CallbackData)
	assert.Equal(t, "reminder:cancel", *kb.InlineKeyboard[0][0].CallbackData)
}

// --- ReminderCalendar ---

func TestReminderCalendar_HeaderRow(t *testing.T) {
	kb := ReminderCalendar(2026, 3, "once", 3)
	rows := kb.InlineKeyboard
	require.GreaterOrEqual(t, len(rows), 3)

	// Row 0: prev / month+year / next
	require.Len(t, rows[0], 3)
	require.NotNil(t, rows[0][0].CallbackData)
	assert.Equal(t, "reminder:cal:prev:once", *rows[0][0].CallbackData)
	require.NotNil(t, rows[0][2].CallbackData)
	assert.Equal(t, "reminder:cal:next:once", *rows[0][2].CallbackData)
}

func TestReminderCalendar_WeekdayRow(t *testing.T) {
	kb := ReminderCalendar(2026, 3, "pp", 3)
	// Row 1: 7 weekday buttons
	require.GreaterOrEqual(t, len(kb.InlineKeyboard), 2)
	assert.Len(t, kb.InlineKeyboard[1], 7)
}

func TestReminderCalendar_FooterRow(t *testing.T) {
	kb := ReminderCalendar(2026, 3, "yr", 3)
	rows := kb.InlineKeyboard
	lastRow := rows[len(rows)-1]
	var cbs []string
	for _, btn := range lastRow {
		if btn.CallbackData != nil {
			cbs = append(cbs, *btn.CallbackData)
		}
	}
	assert.Contains(t, cbs, "reminder:cal:today:yr")
	assert.Contains(t, cbs, "reminder:cancel")
}

func TestReminderCalendar_SelectCallbackFormat(t *testing.T) {
	// March 2026 month in the far future (all days selectable).
	// Use a very past year so all days are "today or future" relative to the calendar.
	kb := ReminderCalendar(2099, 1, "once", 0)
	var selectCbs []string
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, "reminder:cal:select:") {
				selectCbs = append(selectCbs, *btn.CallbackData)
			}
		}
	}
	require.NotEmpty(t, selectCbs)
	// Each select callback: "reminder:cal:select:YYYY-MM-DD:once"
	for _, cb := range selectCbs {
		assert.Contains(t, cb, ":once")
	}
}
