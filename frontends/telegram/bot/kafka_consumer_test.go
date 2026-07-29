package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReminderEvent_JSONRoundTrip(t *testing.T) {
	ev := ReminderEvent{
		UserID:     42,
		Title:      "Позвонить маме",
		ReminderID: 7,
		CreateTask: true,
		TodayDate:  "09-Nov-2025",
		IsActive:   true,
	}

	data, err := json.Marshal(ev)
	require.NoError(t, err)

	var restored ReminderEvent
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, ev, restored)
}

func TestReminderEvent_JSONUnmarshal(t *testing.T) {
	raw := `{"user_id":42,"title":"Test","reminder_id":1,"create_task":false,"today_date":"01-Jan-2025","is_active":true}`
	var ev ReminderEvent
	err := json.Unmarshal([]byte(raw), &ev)
	require.NoError(t, err)
	assert.Equal(t, int64(42), ev.UserID)
	assert.Equal(t, "Test", ev.Title)
	assert.Equal(t, int64(1), ev.ReminderID)
	assert.False(t, ev.CreateTask)
	assert.Equal(t, "01-Jan-2025", ev.TodayDate)
	assert.True(t, ev.IsActive)
}

func TestReminderEvent_JSONUnmarshal_SpecialChars(t *testing.T) {
	raw := `{"user_id":1,"title":"Купить <молоко> & хлеб","reminder_id":1,"create_task":false,"today_date":"","is_active":true}`
	var ev ReminderEvent
	err := json.Unmarshal([]byte(raw), &ev)
	require.NoError(t, err)
	assert.Equal(t, "Купить <молоко> & хлеб", ev.Title)
}

func TestReminderEvent_JSONUnmarshal_EmptyTitle(t *testing.T) {
	raw := `{"user_id":1,"title":"","reminder_id":0,"create_task":false,"today_date":"","is_active":false}`
	var ev ReminderEvent
	err := json.Unmarshal([]byte(raw), &ev)
	require.NoError(t, err)
	assert.Equal(t, "", ev.Title)
	assert.False(t, ev.IsActive)
}

func TestReminderEvent_JSONUnmarshal_InvalidJSON(t *testing.T) {
	raw := `not json at all`
	var ev ReminderEvent
	err := json.Unmarshal([]byte(raw), &ev)
	assert.Error(t, err)
}
