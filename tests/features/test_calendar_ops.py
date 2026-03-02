"""Tests for core/features/calendar_ops.py"""

from pathlib import Path

from core.features.calendar_ops import get_existing_dates, format_date_for_display


# ---------------------------------------------------------------------------
# get_existing_dates
# ---------------------------------------------------------------------------


def _make_daily_dir(tmp_path: Path) -> Path:
    daily = tmp_path / "Daily"
    daily.mkdir()
    return daily


def test_get_existing_dates_empty_dir(tmp_path):
    _make_daily_dir(tmp_path)
    dates = get_existing_dates(tmp_path)
    assert dates == set()


def test_get_existing_dates_finds_md_files(tmp_path):
    daily = _make_daily_dir(tmp_path)
    (daily / "01-Mar-2026.md").write_text("content")
    (daily / "02-Mar-2026.md").write_text("content")

    dates = get_existing_dates(tmp_path)
    assert "01-Mar-2026" in dates
    assert "02-Mar-2026" in dates


def test_get_existing_dates_count(tmp_path):
    daily = _make_daily_dir(tmp_path)
    for name in ("01-Jan-2026.md", "15-Feb-2026.md", "28-Mar-2026.md"):
        (daily / name).write_text("")

    assert len(get_existing_dates(tmp_path)) == 3


def test_get_existing_dates_ignores_non_md(tmp_path):
    daily = _make_daily_dir(tmp_path)
    (daily / "01-Mar-2026.md").write_text("content")
    (daily / "notes.txt").write_text("content")
    (daily / "image.png").write_bytes(b"")

    dates = get_existing_dates(tmp_path)
    assert len(dates) == 1
    assert "01-Mar-2026" in dates


def test_get_existing_dates_no_daily_dir(tmp_path):
    # tmp_path has no Daily/ subdirectory
    dates = get_existing_dates(tmp_path)
    assert dates == set()


def test_get_existing_dates_stems_only(tmp_path):
    daily = _make_daily_dir(tmp_path)
    (daily / "05-Feb-2025.md").write_text("")

    dates = get_existing_dates(tmp_path)
    # Should be "05-Feb-2025", not "05-Feb-2025.md"
    assert "05-Feb-2025" in dates
    assert "05-Feb-2025.md" not in dates


# ---------------------------------------------------------------------------
# format_date_for_display
# ---------------------------------------------------------------------------


def test_format_plain():
    result = format_date_for_display("05-Feb-2025", is_active=False, has_note=False)
    assert result == "05-Feb-2025"


def test_format_has_note():
    result = format_date_for_display("05-Feb-2025", is_active=False, has_note=True)
    assert result == "**05-Feb-2025**"


def test_format_is_active():
    result = format_date_for_display("05-Feb-2025", is_active=True, has_note=False)
    assert result == "[05-Feb-2025]"


def test_format_active_and_has_note():
    result = format_date_for_display("05-Feb-2025", is_active=True, has_note=True)
    assert result == "[**05-Feb-2025**]"
