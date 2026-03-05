"""Tests for notifications/scheduler.py — _compute_next_fire and _build_keyboard."""

from datetime import datetime, timezone

from notifications.scheduler import _build_keyboard, _compute_next_fire

# ---------------------------------------------------------------------------
# Reference time: 2026-03-03 10:00:00 UTC (Tuesday, weekday=1)
# With tz_offset=+3 → local = 2026-03-03 13:00:00
# ---------------------------------------------------------------------------
_REF_UTC = datetime(2026, 3, 3, 10, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# daily
# ---------------------------------------------------------------------------


class TestComputeNextFireDaily:
    def test_same_day_future_time(self):
        """14:00 local (> 13:00 current) → today 11:00 UTC."""
        params = {"hour": 14, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("daily", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 11, 0, 0, tzinfo=timezone.utc)

    def test_past_time_advances_to_tomorrow(self):
        """09:00 local (< 13:00 current) → tomorrow 06:00 UTC."""
        params = {"hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("daily", params, _REF_UTC)
        assert result == datetime(2026, 3, 4, 6, 0, 0, tzinfo=timezone.utc)

    def test_exact_current_time_advances_to_tomorrow(self):
        """13:00 local == current time → must advance to tomorrow."""
        params = {"hour": 13, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("daily", params, _REF_UTC)
        assert result == datetime(2026, 3, 4, 10, 0, 0, tzinfo=timezone.utc)

    def test_midnight_advances_to_next_day(self):
        """00:00 local (< 13:00) → 00:00 next local day = 2026-03-03 21:00 UTC."""
        params = {"hour": 0, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("daily", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 21, 0, 0, tzinfo=timezone.utc)

    def test_default_tz_offset_uses_config_zero(self):
        """No tz_offset in params → uses TIMEZONE_OFFSET_HOURS (0 in notifications/config)."""
        params = {"hour": 15, "minute": 0}
        result = _compute_next_fire("daily", params, _REF_UTC)
        # 15:00 UTC > 10:00 UTC → same day
        assert result == datetime(2026, 3, 3, 15, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# weekly
# ---------------------------------------------------------------------------


class TestComputeNextFireWeekly:
    def test_matching_day_today_future_time(self):
        """Local is Tuesday (1), days=[1], 14:00 > 13:00 → today."""
        params = {"days": [1], "hour": 14, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 11, 0, 0, tzinfo=timezone.utc)

    def test_matching_day_today_past_time_advances_to_next_week(self):
        """Local is Tuesday (1), days=[1], 09:00 < 13:00 → next Tuesday."""
        params = {"days": [1], "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        # 2026-03-10 09:00 +3 = 2026-03-10 06:00 UTC
        assert result == datetime(2026, 3, 10, 6, 0, 0, tzinfo=timezone.utc)

    def test_multiple_days_picks_next_closest(self):
        """days=[0,3] (Mon+Thu): local is Tuesday → next is Thursday."""
        params = {"days": [0, 3], "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        # 2026-03-05 Thursday 09:00 +3 = 06:00 UTC
        assert result == datetime(2026, 3, 5, 6, 0, 0, tzinfo=timezone.utc)

    def test_wraps_around_week_to_monday(self):
        """days=[0] only Monday: local is Tuesday → next Monday."""
        params = {"days": [0], "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        # 2026-03-09 Monday 09:00 +3 = 06:00 UTC
        assert result == datetime(2026, 3, 9, 6, 0, 0, tzinfo=timezone.utc)

    def test_weekend_days(self):
        """days=[5,6] Sat+Sun: local is Tuesday → next Saturday."""
        params = {"days": [5, 6], "hour": 10, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        # 2026-03-07 Saturday 10:00 +3 = 07:00 UTC
        assert result == datetime(2026, 3, 7, 7, 0, 0, tzinfo=timezone.utc)

    def test_all_days_returns_tomorrow(self):
        """days=[0..6]: always fires tomorrow when today's time has passed."""
        params = {"days": list(range(7)), "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("weekly", params, _REF_UTC)
        # 09:00 < 13:00, so advance to tomorrow: Wednesday 2026-03-04 06:00 UTC
        assert result == datetime(2026, 3, 4, 6, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# monthly
# ---------------------------------------------------------------------------


class TestComputeNextFireMonthly:
    def test_same_month_future_day(self):
        """day_of_month=15, current is March 3 → March 15."""
        params = {"day_of_month": 15, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, _REF_UTC)
        assert result == datetime(2026, 3, 15, 6, 0, 0, tzinfo=timezone.utc)

    def test_past_day_same_month_advances_to_next_month(self):
        """day_of_month=1, current is March 3 → April 1."""
        params = {"day_of_month": 1, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, _REF_UTC)
        assert result == datetime(2026, 4, 1, 6, 0, 0, tzinfo=timezone.utc)

    def test_december_overflows_to_january(self):
        """day_of_month=5, current is Dec 10 → Jan 5 next year."""
        ref = datetime(2026, 12, 10, 10, 0, 0, tzinfo=timezone.utc)
        params = {"day_of_month": 5, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, ref)
        assert result == datetime(2027, 1, 5, 6, 0, 0, tzinfo=timezone.utc)

    def test_day_31_in_30_day_month_skips_to_next_valid(self):
        """day_of_month=31, current is April 1 (30 days) → May 31."""
        ref = datetime(2026, 4, 1, 10, 0, 0, tzinfo=timezone.utc)
        params = {"day_of_month": 31, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, ref)
        assert result == datetime(2026, 5, 31, 6, 0, 0, tzinfo=timezone.utc)

    def test_day_31_when_next_month_also_invalid_returns_none(self):
        """day_of_month=31, Jan 31 past time → try Feb 31 → invalid → None."""
        ref = datetime(2026, 1, 31, 10, 0, 0, tzinfo=timezone.utc)  # local 13:00
        params = {"day_of_month": 31, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, ref)
        assert result is None

    def test_same_day_future_time(self):
        """day_of_month=3, current is March 3 13:00 local, fire at 14:00 → today."""
        params = {"day_of_month": 3, "hour": 14, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("monthly", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 11, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# yearly
# ---------------------------------------------------------------------------


class TestComputeNextFireYearly:
    def test_same_year_future_date(self):
        """month=6, day=15, current is March 3 → June 15 this year."""
        params = {"month": 6, "day": 15, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("yearly", params, _REF_UTC)
        assert result == datetime(2026, 6, 15, 6, 0, 0, tzinfo=timezone.utc)

    def test_past_date_this_year_advances_to_next_year(self):
        """month=1, day=15, current is March 3 → Jan 15 next year."""
        params = {"month": 1, "day": 15, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("yearly", params, _REF_UTC)
        assert result == datetime(2027, 1, 15, 6, 0, 0, tzinfo=timezone.utc)

    def test_feb_29_in_non_leap_year_returns_none(self):
        """Feb 29 in 2026 (non-leap) and 2027 (non-leap) → None."""
        params = {"month": 2, "day": 29, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("yearly", params, _REF_UTC)
        assert result is None

    def test_feb_29_advances_to_next_leap_year(self):
        """Feb 29 from 2027 (non-leap) → 2028 (leap year)."""
        ref = datetime(2027, 3, 1, 10, 0, 0, tzinfo=timezone.utc)
        params = {"month": 2, "day": 29, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("yearly", params, ref)
        assert result == datetime(2028, 2, 29, 6, 0, 0, tzinfo=timezone.utc)

    def test_same_day_future_time(self):
        """month=3, day=3, current is March 3 13:00 local, fire at 14:00 → today."""
        params = {"month": 3, "day": 3, "hour": 14, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("yearly", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 11, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# custom_days
# ---------------------------------------------------------------------------


class TestComputeNextFireCustomDays:
    def test_same_day_future_time(self):
        """14:00 local > 13:00 current → today."""
        params = {"interval_days": 3, "hour": 14, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("custom_days", params, _REF_UTC)
        assert result == datetime(2026, 3, 3, 11, 0, 0, tzinfo=timezone.utc)

    def test_past_time_advances_by_n_days(self):
        """09:00 local passed → 3 days later at 09:00."""
        params = {"interval_days": 3, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("custom_days", params, _REF_UTC)
        assert result == datetime(2026, 3, 6, 6, 0, 0, tzinfo=timezone.utc)

    def test_interval_one_day(self):
        """interval_days=1, past time → tomorrow."""
        params = {"interval_days": 1, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("custom_days", params, _REF_UTC)
        assert result == datetime(2026, 3, 4, 6, 0, 0, tzinfo=timezone.utc)

    def test_large_interval(self):
        """interval_days=30, past time → 30 days from today."""
        params = {"interval_days": 30, "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("custom_days", params, _REF_UTC)
        # 2026-03-03 + 30 = 2026-04-02 09:00 +3 = 06:00 UTC
        assert result == datetime(2026, 4, 2, 6, 0, 0, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# once
# ---------------------------------------------------------------------------


class TestComputeNextFireOnce:
    def test_always_returns_none(self):
        """Once type always returns None — deactivated after firing."""
        params = {"date": "2026-03-10", "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_next_fire("once", params, _REF_UTC)
        assert result is None

    def test_returns_none_regardless_of_params(self):
        result = _compute_next_fire("once", {}, _REF_UTC)
        assert result is None


# ---------------------------------------------------------------------------
# unknown type
# ---------------------------------------------------------------------------


class TestComputeNextFireUnknown:
    def test_unknown_type_returns_none(self):
        result = _compute_next_fire("biweekly", {}, _REF_UTC)
        assert result is None

    def test_empty_string_returns_none(self):
        result = _compute_next_fire("", {}, _REF_UTC)
        assert result is None


# ---------------------------------------------------------------------------
# _build_keyboard
# ---------------------------------------------------------------------------


class TestBuildKeyboard:
    def test_has_four_rows(self):
        kb = _build_keyboard(42)
        assert len(kb["inline_keyboard"]) == 4

    def test_done_button_callback_no_task(self):
        kb = _build_keyboard(42)
        btn = kb["inline_keyboard"][0][0]
        assert btn["callback_data"] == "reminder:done:42:0"

    def test_done_button_callback_with_task(self):
        kb = _build_keyboard(42, create_task=True, today_date="05-Mar-2026")
        btn = kb["inline_keyboard"][0][0]
        assert btn["callback_data"] == "reminder:done:42:1:05-Mar-2026"

    def test_postpone_hours_row(self):
        kb = _build_keyboard(42)
        row = kb["inline_keyboard"][1]
        assert row[0]["callback_data"] == "reminder:postpone_hours:1:42"
        assert row[1]["callback_data"] == "reminder:postpone_hours:3:42"

    def test_postpone_one_day_callback(self):
        kb = _build_keyboard(42)
        btn = kb["inline_keyboard"][2][0]
        assert btn["callback_data"] == "reminder:postpone:1:42"

    def test_postpone_three_days_callback(self):
        kb = _build_keyboard(42)
        btn = kb["inline_keyboard"][2][1]
        assert btn["callback_data"] == "reminder:postpone:3:42"

    def test_custom_date_callback(self):
        kb = _build_keyboard(42)
        btn = kb["inline_keyboard"][3][0]
        assert btn["callback_data"] == "reminder:custom_date:42"

    def test_id_embedded_in_all_action_callbacks(self):
        kb = _build_keyboard(777)
        all_cbs = [btn["callback_data"] for row in kb["inline_keyboard"] for btn in row]
        action_cbs = [cb for cb in all_cbs if cb != "reminder:noop"]
        assert all("777" in cb for cb in action_cbs)

    def test_done_button_has_checkmark_text(self):
        kb = _build_keyboard(1)
        btn = kb["inline_keyboard"][0][0]
        assert "✅" in btn["text"]
