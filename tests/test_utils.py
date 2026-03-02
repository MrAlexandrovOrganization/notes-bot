"""Tests for core/utils.py — get_today_filename()"""

from datetime import datetime, timezone
from unittest.mock import patch

from core.utils import get_today_filename

# core.utils imports TIMEZONE_OFFSET_HOURS=3, DAY_START_HOUR=7 from the
# mocked core.config injected by conftest.py.


def _utc(year, month, day, hour, minute=0):
    return datetime(year, month, day, hour, minute, tzinfo=timezone.utc)


def test_midday_utc_returns_today():
    # 12:00 UTC → 15:00 Moscow → same day
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 12, 0)
        result = get_today_filename()
    assert result == "03-Mar-2026.md"


def test_evening_utc_returns_today():
    # 20:00 UTC → 23:00 Moscow → same day
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 20, 0)
        result = get_today_filename()
    assert result == "03-Mar-2026.md"


def test_early_morning_utc_returns_previous_day():
    # 02:00 UTC → 05:00 Moscow → before 07:00 → previous day
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 2, 0)
        result = get_today_filename()
    assert result == "02-Mar-2026.md"


def test_exactly_at_day_start_returns_today():
    # 04:00 UTC → 07:00 Moscow → exactly DAY_START_HOUR → today
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 4, 0)
        result = get_today_filename()
    assert result == "03-Mar-2026.md"


def test_one_minute_before_day_start_returns_previous_day():
    # 03:59 UTC → 06:59 Moscow → before 07:00 → previous day
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 3, 59)
        result = get_today_filename()
    assert result == "02-Mar-2026.md"


def test_midnight_utc_crosses_month_boundary():
    # 01:00 UTC → 04:00 Moscow on March 1 → before 07:00 → Feb 28
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 1, 1, 0)
        result = get_today_filename()
    assert result == "28-Feb-2026.md"


def test_result_has_md_extension():
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 3, 3, 12, 0)
        result = get_today_filename()
    assert result.endswith(".md")


def test_result_format():
    # Should be DD-Mon-YYYY.md (e.g. 03-Mar-2026.md)
    with patch("core.utils.datetime") as mock_dt:
        mock_dt.now.return_value = _utc(2026, 1, 5, 12, 0)
        result = get_today_filename()
    assert result == "05-Jan-2026.md"
