"""Tests for core/notes.py"""

from pathlib import Path
from unittest.mock import patch

from core.notes import read_note, create_daily_note_from_template, save_message

TEMPLATE_CONTENT = (
    "---\n"
    'date: "[[{{date:DD-MMM-YYYY}}]]"\n'
    'title: "[[{{date:DD-MMM-YYYY}}]]"\n'
    "tags:\n"
    "  - daily\n"
    "Оценка:\n"
    "---\n"
    "- [ ] Доброго утра!\n"
    "---\n\n"
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def make_notes_dir(tmp_path: Path):
    daily = tmp_path / "Daily"
    daily.mkdir()
    templates = tmp_path / "Templates"
    templates.mkdir()
    template_file = templates / "Daily.md"
    template_file.write_text(TEMPLATE_CONTENT, encoding="utf-8")
    return daily, template_file


# ---------------------------------------------------------------------------
# read_note
# ---------------------------------------------------------------------------


def test_read_note_returns_content(tmp_path):
    daily, _ = make_notes_dir(tmp_path)
    note = daily / "01-Mar-2026.md"
    note.write_text("hello world", encoding="utf-8")

    with patch("core.notes.DAILY_NOTES_DIR", daily):
        result = read_note("01-Mar-2026.md")

    assert result == "hello world"


def test_read_note_missing_file_returns_none(tmp_path):
    daily, _ = make_notes_dir(tmp_path)

    with patch("core.notes.DAILY_NOTES_DIR", daily):
        result = read_note("nonexistent.md")

    assert result is None


def test_read_note_unicode_content(tmp_path):
    daily, _ = make_notes_dir(tmp_path)
    content = "Привет мир\n- [ ] Задача"
    (daily / "01-Mar-2026.md").write_text(content, encoding="utf-8")

    with patch("core.notes.DAILY_NOTES_DIR", daily):
        result = read_note("01-Mar-2026.md")

    assert result == content


# ---------------------------------------------------------------------------
# create_daily_note_from_template
# ---------------------------------------------------------------------------


def test_create_from_template_creates_file(tmp_path):
    daily, template = make_notes_dir(tmp_path)
    filepath = daily / "01-Mar-2026.md"

    with patch("core.notes.DAILY_TEMPLATE_PATH", template):
        create_daily_note_from_template(filepath, "01-Mar-2026")

    assert filepath.exists()


def test_create_from_template_substitutes_date(tmp_path):
    daily, template = make_notes_dir(tmp_path)
    filepath = daily / "15-Apr-2026.md"

    with patch("core.notes.DAILY_TEMPLATE_PATH", template):
        create_daily_note_from_template(filepath, "15-Apr-2026")

    content = filepath.read_text(encoding="utf-8")
    assert "15-Apr-2026" in content
    assert "{{date:DD-MMM-YYYY}}" not in content


def test_create_fallback_when_template_missing(tmp_path):
    daily, _ = make_notes_dir(tmp_path)
    missing_template = tmp_path / "Templates" / "NoSuch.md"
    filepath = daily / "01-Mar-2026.md"

    with patch("core.notes.DAILY_TEMPLATE_PATH", missing_template):
        create_daily_note_from_template(filepath, "01-Mar-2026")

    assert filepath.exists()
    content = filepath.read_text(encoding="utf-8")
    assert "01-Mar-2026" in content
    assert "- [ ]" in content


# ---------------------------------------------------------------------------
# save_message
# ---------------------------------------------------------------------------


def test_save_message_appends_to_existing_note(tmp_path):
    daily, template = make_notes_dir(tmp_path)
    note = daily / "01-Mar-2026.md"
    note.write_text("existing content\n", encoding="utf-8")

    with (
        patch("core.notes.DAILY_NOTES_DIR", daily),
        patch("core.notes.DAILY_TEMPLATE_PATH", template),
        patch("core.notes.get_today_filename", return_value="01-Mar-2026.md"),
    ):
        save_message("new line")

    content = note.read_text(encoding="utf-8")
    assert "existing content" in content
    assert "new line" in content


def test_save_message_creates_note_when_missing(tmp_path):
    daily, template = make_notes_dir(tmp_path)

    with (
        patch("core.notes.DAILY_NOTES_DIR", daily),
        patch("core.notes.DAILY_TEMPLATE_PATH", template),
        patch("core.notes.get_today_filename", return_value="01-Mar-2026.md"),
    ):
        save_message("hello")

    note = daily / "01-Mar-2026.md"
    assert note.exists()
    assert "hello" in note.read_text(encoding="utf-8")


def test_save_message_multiple_messages(tmp_path):
    daily, template = make_notes_dir(tmp_path)

    with (
        patch("core.notes.DAILY_NOTES_DIR", daily),
        patch("core.notes.DAILY_TEMPLATE_PATH", template),
        patch("core.notes.get_today_filename", return_value="01-Mar-2026.md"),
    ):
        save_message("first")
        save_message("second")
        save_message("third")

    content = (daily / "01-Mar-2026.md").read_text(encoding="utf-8")
    assert "first" in content
    assert "second" in content
    assert "third" in content
