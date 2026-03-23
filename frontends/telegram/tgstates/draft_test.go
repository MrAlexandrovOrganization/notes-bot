package tgstates

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unmarshalParams(t *testing.T, jsonStr string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &m))
	return m
}

func TestToParamsJSON_Daily(t *testing.T) {
	d := ReminderDraft{ScheduleType: "daily", Hour: 9, Minute: 30}
	got, err := d.ToParamsJSON(3)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, float64(9), p["hour"])
	assert.Equal(t, float64(30), p["minute"])
	assert.Equal(t, float64(3), p["tz_offset"])
}

func TestToParamsJSON_Weekly(t *testing.T) {
	d := ReminderDraft{ScheduleType: "weekly", Hour: 8, Minute: 0, Days: []int{0, 2, 4}}
	got, err := d.ToParamsJSON(3)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	days, ok := p["days"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{float64(0), float64(2), float64(4)}, days)
}

func TestToParamsJSON_Monthly(t *testing.T) {
	d := ReminderDraft{ScheduleType: "monthly", Hour: 10, Minute: 0, DayOfMonth: 15}
	got, err := d.ToParamsJSON(3)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, float64(15), p["day_of_month"])
}

func TestToParamsJSON_Yearly(t *testing.T) {
	d := ReminderDraft{ScheduleType: "yearly", Hour: 12, Minute: 0, Month: 6, Day: 1}
	got, err := d.ToParamsJSON(3)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, float64(6), p["month"])
	assert.Equal(t, float64(1), p["day"])
}

func TestToParamsJSON_Once(t *testing.T) {
	d := ReminderDraft{ScheduleType: "once", Date: "2025-12-31"}
	got, err := d.ToParamsJSON(0)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, "2025-12-31", p["date"])
}

func TestToParamsJSON_CustomDays(t *testing.T) {
	d := ReminderDraft{ScheduleType: "custom_days", Hour: 7, Minute: 0, IntervalDays: 3}
	got, err := d.ToParamsJSON(3)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, float64(3), p["interval_days"])
}

func TestToParamsJSON_TzOffset(t *testing.T) {
	d := ReminderDraft{Hour: 9}
	got, err := d.ToParamsJSON(5)
	require.NoError(t, err)
	p := unmarshalParams(t, got)
	assert.Equal(t, float64(5), p["tz_offset"])
}

func TestToParamsJSON_NoDaysOmitted(t *testing.T) {
	// When Days is nil it should be omitted from JSON.
	d := ReminderDraft{ScheduleType: "daily", Hour: 9}
	got, err := d.ToParamsJSON(0)
	require.NoError(t, err)
	assert.NotContains(t, got, `"days"`)
}
