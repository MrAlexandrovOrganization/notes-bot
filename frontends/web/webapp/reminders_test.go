package webapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notes-bot/frontends/web/views"
)

func TestFormToScheduleParams_Daily(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "daily", Hour: 9, Minute: 30}, 3)
	require.NoError(t, err)
	assert.EqualValues(t, 9, sp.Hour)
	assert.EqualValues(t, 30, sp.Minute)
	assert.EqualValues(t, 3, sp.TzOffset)
	assert.Nil(t, sp.Extra)
}

func TestFormToScheduleParams_Weekly(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "weekly", Hour: 9, Minute: 0, Days: []int{0, 4}}, 3)
	require.NoError(t, err)
	weekly := sp.GetWeekly()
	require.NotNil(t, weekly)
	assert.EqualValues(t, []int32{0, 4}, weekly.Days)
}

func TestFormToScheduleParams_WeeklyRequiresDays(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "weekly", Hour: 9}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_WeeklyRejectsOutOfRangeDay(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "weekly", Hour: 9, Days: []int{7}}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_Monthly(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "monthly", Hour: 10, DayOfMonth: 25}, 3)
	require.NoError(t, err)
	assert.EqualValues(t, 25, sp.GetMonthly().DayOfMonth)
}

func TestFormToScheduleParams_MonthlyRejectsOutOfRange(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "monthly", Hour: 10, DayOfMonth: 32}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_Yearly(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "yearly", Hour: 20, Month: 6, Day: 2}, 3)
	require.NoError(t, err)
	assert.EqualValues(t, 6, sp.GetYearly().Month)
	assert.EqualValues(t, 2, sp.GetYearly().Day)
}

func TestFormToScheduleParams_Once(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "once", Hour: 8, Date: "2026-08-01"}, 3)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-01", sp.GetOnce().Date)
}

func TestFormToScheduleParams_OnceRequiresDate(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "once", Hour: 8}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_CustomDays(t *testing.T) {
	sp, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "custom_days", Hour: 7, IntervalDays: 3}, 3)
	require.NoError(t, err)
	assert.EqualValues(t, 3, sp.GetCustomDays().IntervalDays)
}

func TestFormToScheduleParams_CustomDaysRejectsZero(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "custom_days", Hour: 7, IntervalDays: 0}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_InvalidTime(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "daily", Hour: 24, Minute: 0}, 3)
	assert.Error(t, err)
	_, err = formToScheduleParams(views.ReminderFormData{ScheduleType: "daily", Hour: -1, Minute: 0}, 3)
	assert.Error(t, err)
}

func TestFormToScheduleParams_UnknownType(t *testing.T) {
	_, err := formToScheduleParams(views.ReminderFormData{ScheduleType: "bogus", Hour: 9}, 3)
	assert.Error(t, err)
}

func TestParseDuration_BareMinutes(t *testing.T) {
	n, err := parseDuration("90")
	require.NoError(t, err)
	assert.Equal(t, 90, n)
}

func TestParseDuration_CompositeUnits(t *testing.T) {
	cases := map[string]int{
		"30m":     30,
		"2h30m":   150,
		"1d12h":   2160,
		"1w":      10080,
		"1M":      43200,
		"3d6h30m": 3*1440 + 6*60 + 30,
	}
	for input, want := range cases {
		n, err := parseDuration(input)
		require.NoError(t, err, input)
		assert.Equal(t, want, n, input)
	}
}

func TestParseDuration_RejectsOverflow(t *testing.T) {
	_, err := parseDuration("90m")
	assert.Error(t, err)
	_, err = parseDuration("27h")
	assert.Error(t, err)
	_, err = parseDuration("8d")
	assert.Error(t, err)
}

func TestParseDuration_RejectsInvalid(t *testing.T) {
	_, err := parseDuration("")
	assert.Error(t, err)
	_, err = parseDuration("abc")
	assert.Error(t, err)
	_, err = parseDuration("5x")
	assert.Error(t, err)
	_, err = parseDuration("5m5m")
	assert.Error(t, err)
	_, err = parseDuration("0")
	assert.Error(t, err)
	_, err = parseDuration("-5")
	assert.Error(t, err)
}
