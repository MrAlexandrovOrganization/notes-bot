package notifications

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	result := ComputeNextFire(context.Background(), "once", map[string]any{}, utc("2025-11-09 10:00"), tzMoscow)
	assert.Nil(t, result, "once schedule should return nil (deactivate)")
}

func TestComputeNextFire_Daily_SameDay(t *testing.T) {
	// Local time is 2025-11-09 10:00 UTC+3 = 13:00 local.
	// Fire at 09:00 local → already passed, so next fire is 2025-11-10 09:00 local = 06:00 UTC.
	after := utc("2025-11-09 10:00") // 13:00 Moscow
	params := map[string]any{"hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "daily", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-10 06:00")
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Daily_LaterSameDay(t *testing.T) {
	// Local 06:00 Moscow (03:00 UTC). Fire at 09:00 local → same day still ahead.
	after := utc("2025-11-09 03:00") // 06:00 Moscow
	params := map[string]any{"hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "daily", params, after, tzMoscow)
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
	got := ComputeNextFire(context.Background(), "weekly", params, after, tzMoscow)
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
	got := ComputeNextFire(context.Background(), "weekly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-08 07:00") // 10:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Monthly(t *testing.T) {
	// Current: 2025-11-15. Fire on day 10 each month → next is 2025-12-10.
	after := utc("2025-11-15 10:00")
	params := map[string]any{"day_of_month": 10, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-12-10 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Monthly_SameDayBefore(t *testing.T) {
	// Current: 2025-11-10 06:00 UTC (09:00 Moscow). Fire at 10:00 → same day ahead.
	after := utc("2025-11-10 06:00")
	params := map[string]any{"day_of_month": 10, "hour": 10, "minute": 0}
	got := ComputeNextFire(context.Background(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-10 07:00") // 10:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Yearly(t *testing.T) {
	// Fire every March 8. Current: 2025-11-09 → next is 2026-03-08.
	after := utc("2025-11-09 10:00")
	params := map[string]any{"month": 3, "day": 8, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "yearly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2026-03-08 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_Yearly_SameYearAhead(t *testing.T) {
	// Fire every March 8. Current: 2026-01-01 → fire is 2026-03-08.
	after := utc("2026-01-01 10:00")
	params := map[string]any{"month": 3, "day": 8, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "yearly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2026-03-08 06:00")
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_CustomDays(t *testing.T) {
	// Every 3 days at 09:00. Current: 08:00 Moscow → same day fire at 09:00.
	after := utc("2025-11-09 05:00") // 08:00 Moscow
	params := map[string]any{"interval_days": 3, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "custom_days", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-09 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_CustomDays_AlreadyPassed(t *testing.T) {
	// Every 3 days at 09:00. Current: 13:00 Moscow → fire in 3 days.
	after := utc("2025-11-09 10:00") // 13:00 Moscow
	params := map[string]any{"interval_days": 3, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "custom_days", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-11-12 06:00") // 2025-11-09+3 at 09:00 Moscow
	assert.Equal(t, want, *got)
}

func TestComputeNextFire_UnknownType(t *testing.T) {
	result := ComputeNextFire(context.Background(), "unknown_type", map[string]any{}, utc("2025-11-09 10:00"), tzMoscow)
	assert.Nil(t, result)
}

func TestParamInt_Defaults(t *testing.T) {
	assert.Equal(t, 9, paramInt(map[string]any{}, "hour", 9))
	assert.Equal(t, 5, paramInt(map[string]any{"hour": float64(5)}, "hour", 9))
	assert.Equal(t, 7, paramInt(map[string]any{"hour": 7}, "hour", 9))
}

func TestParamIntSlice_Defaults(t *testing.T) {
	assert.Equal(t, []int{0}, paramIntSlice(context.Background(), map[string]any{}, "days", []int{0}))
	assert.Equal(t, []int{1, 3, 5}, paramIntSlice(context.Background(), map[string]any{
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
	result := paramIntSlice(context.Background(), map[string]any{
		"days": []any{1, 3, 5},
	}, "days", nil)
	assert.Equal(t, []int{1, 3, 5}, result)
}

func TestParamIntSlice_NotSlice_ReturnsDefault(t *testing.T) {
	assert.Equal(t, []int{0}, paramIntSlice(context.Background(), map[string]any{
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

// --- Monthly: day doesn't exist in next month (returns nil) ---

func TestComputeNextFire_Monthly_NextMonthHasNoSuchDay(t *testing.T) {
	// After Jan 31 13:00 Moscow, day_of_month=31.
	// Current month Jan 31 09:00 already passed → advance to Feb.
	// Feb has no day 31 → safeDate returns nil → ComputeNextFire returns nil.
	after := utc("2025-01-31 10:00") // 13:00 Moscow
	params := map[string]any{"day_of_month": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "monthly", params, after, tzMoscow)
	assert.Nil(t, got)
}

func TestComputeNextFire_Monthly_CurrentMonthNoSuchDay(t *testing.T) {
	// Feb 15, day_of_month=31 → Feb has no 31 → advance to March 31.
	after := utc("2025-02-15 10:00") // 13:00 Moscow
	params := map[string]any{"day_of_month": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "monthly", params, after, tzMoscow)
	require.NotNil(t, got)
	want := utc("2025-03-31 06:00") // 09:00 Moscow
	assert.Equal(t, want, *got)
}

// --- Weekly: no matching days (returns nil) ---

func TestComputeNextFire_Weekly_EmptyDays(t *testing.T) {
	after := utc("2025-11-09 10:00")
	params := map[string]any{"days": []any{}, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "weekly", params, after, tzMoscow)
	assert.Nil(t, got)
}

// --- Yearly: invalid date (returns nil) ---

func TestComputeNextFire_Yearly_InvalidDate(t *testing.T) {
	// Feb 31 doesn't exist any year → nil.
	after := utc("2025-11-09 10:00")
	params := map[string]any{"month": 2, "day": 31, "hour": 9, "minute": 0}
	got := ComputeNextFire(context.Background(), "yearly", params, after, tzMoscow)
	assert.Nil(t, got)
}
