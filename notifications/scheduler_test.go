package notifications

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/notifications"
)

const tzMoscow = 3 // UTC+3

// utc parses a UTC time string for use in tests.
func utc(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func TestComputeNextFire_Once(t *testing.T) {
	result := ComputeNextFire(t.Context(), "once", map[string]any{}, utc("2025-11-09 10:00"), tzMoscow)
	assert.Nil(t, result, "once schedule should return nil (deactivate)")
}

func TestComputeNextFire_Daily_SameDay(t *testing.T) {
	// Local time is 2025-11-09 10:00 UTC+3 = 13:00 local.
	// Fire at 09:00 local → already passed, so next fire is 2025-11-10 09:00 local = 06:00 UTC.
	after := utc("2025-11-09 10:00") // 13:00 Moscow
	params := map[string]any{"hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "daily", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-10 06:00")
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Daily_LaterSameDay(t *testing.T) {
	// Local 06:00 Moscow (03:00 UTC). Fire at 09:00 local → same day still ahead.
	after := utc("2025-11-09 03:00") // 06:00 Moscow
	params := map[string]any{"hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "daily", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-09 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Weekly_ThisWeek(t *testing.T) {
	// 2025-11-09 is Saturday (weekday=5 in Monday=0 scheme).
	// Fire every Monday (0) at 09:00 local. Next is 2025-11-10 (Sunday? no).
	// Let's use Wednesday=2. 2025-11-09 Sat → next Wed is 2025-11-12.
	after := utc("2025-11-09 10:00") // Saturday Moscow afternoon
	params := map[string]any{"days": []any{float64(2)}, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "weekly", params, after, tzMoscow)
	require.NotNil(t, got)
	// 2025-11-12 Wednesday 09:00 Moscow = 06:00 UTC
	want := utc("2025-11-12 06:00")
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Weekly_TodayAfter(t *testing.T) {
	// 2025-11-08 is Saturday (weekday=5 in Mon=0 scheme).
	// Fire on Saturday (5) at 10:00 Moscow. Current: 08:00 Moscow → same day slot ahead.
	after := utc("2025-11-08 05:00") // 08:00 Moscow (Saturday)
	params := map[string]any{"days": []any{float64(5)}, "hour": 10, "minute": 0}
	got := ComputeNextFire(t.Context(), "weekly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-08 07:00") // 10:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Monthly(t *testing.T) {
	// Current: 2025-11-15. Fire on day 10 each month → next is 2025-12-10.
	after := utc("2025-11-15 10:00")
	params := map[string]any{"day_of_month": 10, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-12-10 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Monthly_SameDayBefore(t *testing.T) {
	// Current: 2025-11-10 06:00 UTC (09:00 Moscow). Fire at 10:00 → same day ahead.
	after := utc("2025-11-10 06:00")
	params := map[string]any{"day_of_month": 10, "hour": 10, "minute": 0}
	got := ComputeNextFire(t.Context(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-10 07:00") // 10:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Yearly(t *testing.T) {
	// Fire every March 8. Current: 2025-11-09 → next is 2026-03-08.
	after := utc("2025-11-09 10:00")
	params := map[string]any{"month": 3, "day": 8, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "yearly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2026-03-08 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Yearly_SameYearAhead(t *testing.T) {
	// Fire every March 8. Current: 2026-01-01 → fire is 2026-03-08.
	after := utc("2026-01-01 10:00")
	params := map[string]any{"month": 3, "day": 8, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "yearly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2026-03-08 06:00")
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_CustomDays(t *testing.T) {
	// Every 3 days at 09:00. Current: 08:00 Moscow → same day fire at 09:00.
	after := utc("2025-11-09 05:00") // 08:00 Moscow
	params := map[string]any{"interval_days": 3, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "custom_days", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-09 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_CustomDays_AlreadyPassed(t *testing.T) {
	// Every 3 days at 09:00. Current: 13:00 Moscow → fire in 3 days.
	after := utc("2025-11-09 10:00") // 13:00 Moscow
	params := map[string]any{"interval_days": 3, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "custom_days", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-12 06:00") // 2025-11-09+3 at 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_UnknownType(t *testing.T) {
	result := ComputeNextFire(t.Context(), "unknown_type", map[string]any{}, utc("2025-11-09 10:00"), tzMoscow)
	assert.Nil(t, result)
}

func TestParamInt_Defaults(t *testing.T) {
	assert.Equal(t, 9, paramInt(map[string]any{}, "hour", 9))
	assert.Equal(t, 5, paramInt(map[string]any{"hour": float64(5)}, "hour", 9))
	assert.Equal(t, 7, paramInt(map[string]any{"hour": 7}, "hour", 9))
}

func TestParamIntSlice_Defaults(t *testing.T) {
	assert.Equal(t, []int{0}, paramIntSlice(map[string]any{}, "days", []int{0}))
	assert.Equal(t, []int{1, 3, 5}, paramIntSlice(map[string]any{
		"days": []any{float64(1), float64(3), float64(5)},
	}, "days", nil))
}

// --- paramInt edge cases ---

func TestParamInt_Int64Type(t *testing.T) {
	assert.Equal(t, 5, paramInt(map[string]any{"hour": int64(5)}, "hour", 9))
}

func TestParamInt_JsonNumber(t *testing.T) {
	assert.Equal(t, 7, paramInt(map[string]any{"hour": json.Number("7")}, "hour", 9))
}

func TestParamInt_UnknownType_ReturnsDefault(t *testing.T) {
	assert.Equal(t, 9, paramInt(map[string]any{"hour": "nine"}, "hour", 9))
}

func TestParamInt_InvalidJsonNumber_ReturnsDefault(t *testing.T) {
	assert.Equal(t, 9, paramInt(map[string]any{"hour": json.Number("not-a-number")}, "hour", 9))
}

// --- paramIntSlice with int type ---

func TestParamIntSlice_IntType(t *testing.T) {
	result := paramIntSlice(map[string]any{
		"days": []any{1, 3, 5},
	}, "days", nil)
	assert.Equal(t, []int{1, 3, 5}, result)
}

// Regression: scheduleParamsToMap stores days as []int; the first
// ComputeNextFire call must not silently fall back to the default [Monday].
func TestParamIntSlice_TypedIntSlice(t *testing.T) {
	result := paramIntSlice(map[string]any{
		"days": []int{2, 4},
	}, "days", []int{0})
	assert.Equal(t, []int{2, 4}, result)
}

func TestComputeNextFire_Weekly_TypedIntDays(t *testing.T) {
	// Regression: []int (not []any) used to be ignored → default Monday.
	after := utc("2025-11-09 10:00") // Saturday
	params := map[string]any{"days": []int{2}, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "weekly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-12 06:00") // Wednesday 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestParamIntSlice_NotSlice_ReturnsDefault(t *testing.T) {
	assert.Equal(t, []int{0}, paramIntSlice(map[string]any{
		"days": "monday",
	}, "days", []int{0}))
}

// --- safeDate ---

func TestSafeDate_DayOverflow(t *testing.T) {
	result := safeDate(2025, time.February, 30, 9, 0, time.UTC)
	assert.Nil(t, result, "Feb has at most 28/29 days")
}

func TestSafeDate_ValidDate(t *testing.T) {
	result := safeDate(2025, time.March, 31, 9, 0, time.UTC)
	assert.NotNil(t, result)
}

// --- Monthly: day doesn't exist in some months → skip to next month with it ---

func TestComputeNextFire_Monthly_NextMonthHasNoSuchDay(t *testing.T) {
	// After Jan 31 13:00 Moscow, day_of_month=31.
	// Feb has no day 31 → skip to Mar 31 (cron semantics, no deactivation).
	after := utc("2025-01-31 10:00") // 13:00 Moscow
	params := map[string]any{"day_of_month": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-03-31 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Yearly_Feb29_SkipsNonLeapYears(t *testing.T) {
	// After Mar 1 2025, Feb 29 already passed this (non-leap) year → next is Feb 29 2028.
	after := utc("2025-03-01 10:00")
	params := map[string]any{"month": 2, "day": 29, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "yearly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2028-02-29 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Monthly_CurrentMonthNoSuchDay(t *testing.T) {
	// Feb 15, day_of_month=31 → Feb has no 31 → advance to March 31.
	after := utc("2025-02-15 10:00") // 13:00 Moscow
	params := map[string]any{"day_of_month": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-03-31 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

// --- Weekly: no matching days (returns nil) ---

func TestComputeNextFire_Weekly_EmptyDays(t *testing.T) {
	after := utc("2025-11-09 10:00")
	params := map[string]any{"days": []any{}, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "weekly", params, after, tzMoscow)
	assert.Nil(t, got)
}

// --- Yearly: invalid date (returns nil) ---

func TestComputeNextFire_Yearly_InvalidDate(t *testing.T) {
	// Feb 31 doesn't exist any year → nil.
	after := utc("2025-11-09 10:00")
	params := map[string]any{"month": 2, "day": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(t.Context(), "yearly", params, after, tzMoscow)
	assert.Nil(t, got)
}

// --- CreateReminder request validation ---

func TestValidateReminderRequest(t *testing.T) {
	ok := func() *pb.ScheduleParams {
		return &pb.ScheduleParams{Hour: 9, Minute: 0}
	}
	assert.NoError(t, validateReminderRequest("daily", ok()))
	// typed []int32 weekly days accepted
	assert.NoError(t, validateReminderRequest("weekly", &pb.ScheduleParams{
		Hour: 9, Minute: 0,
		Extra: &pb.ScheduleParams_Weekly{Weekly: &pb.ScheduleParams_WeeklyExtra{Days: []int32{1, 3}}},
	}))
	assert.Error(t, validateReminderRequest("unknown", ok()), "unknown schedule_type")
	assert.Error(t, validateReminderRequest("daily", nil))
	badHour := ok()
	badHour.Hour = 25
	assert.Error(t, validateReminderRequest("daily", badHour), "hour out of range")
	noDays := &pb.ScheduleParams{Hour: 9, Extra: &pb.ScheduleParams_Weekly{Weekly: &pb.ScheduleParams_WeeklyExtra{}}}
	assert.Error(t, validateReminderRequest("weekly", noDays), "weekly without days")
	badInterval := &pb.ScheduleParams{Hour: 9, Extra: &pb.ScheduleParams_CustomDays{CustomDays: &pb.ScheduleParams_CustomDaysExtra{IntervalDays: 0}}}
	assert.Error(t, validateReminderRequest("custom_days", badInterval), "interval_days must be >= 1")
}
