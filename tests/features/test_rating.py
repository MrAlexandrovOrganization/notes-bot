"""Tests for core/features/rating.py"""

from core.features.rating import (
    get_rating_impl,
    update_rating_impl,
    get_rating,
    update_rating,
)

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

FRONTMATTER_WITH_RATING = '---\ndate: "[[01-Mar-2026]]"\nОценка: 7\n---\ncontent\n'
FRONTMATTER_WITHOUT_RATING = (
    '---\ndate: "[[01-Mar-2026]]"\ntags:\n  - daily\n---\ncontent\n'
)
FRONTMATTER_EMPTY_RATING = "---\nОценка:\n---\ncontent\n"
INVALID_FRONTMATTER = "no delimiters here"


# ---------------------------------------------------------------------------
# get_rating_impl
# ---------------------------------------------------------------------------


def test_get_rating_impl_returns_value():
    assert get_rating_impl(FRONTMATTER_WITH_RATING) == 7


def test_get_rating_impl_no_rating_field():
    assert get_rating_impl(FRONTMATTER_WITHOUT_RATING) is None


def test_get_rating_impl_empty_rating_value():
    assert get_rating_impl(FRONTMATTER_EMPTY_RATING) is None


def test_get_rating_impl_invalid_frontmatter():
    assert get_rating_impl(INVALID_FRONTMATTER) is None


def test_get_rating_impl_zero():
    content = "---\nОценка: 0\n---\ncontent\n"
    assert get_rating_impl(content) == 0


def test_get_rating_impl_ten():
    content = "---\nОценка: 10\n---\ncontent\n"
    assert get_rating_impl(content) == 10


# ---------------------------------------------------------------------------
# update_rating_impl
# ---------------------------------------------------------------------------


def test_update_rating_impl_updates_existing_field():
    result = update_rating_impl(FRONTMATTER_WITH_RATING, 3)
    assert result is not None
    assert "Оценка: 3" in result
    assert "Оценка: 7" not in result


def test_update_rating_impl_adds_field_when_missing():
    result = update_rating_impl(FRONTMATTER_WITHOUT_RATING, 5)
    assert result is not None
    assert "Оценка: 5" in result


def test_update_rating_impl_roundtrip():
    result = update_rating_impl(FRONTMATTER_WITH_RATING, 9)
    assert result is not None
    assert get_rating_impl(result) == 9


def test_update_rating_impl_invalid_frontmatter():
    assert update_rating_impl(INVALID_FRONTMATTER, 5) is None


def test_update_rating_impl_preserves_other_fields():
    result = update_rating_impl(FRONTMATTER_WITH_RATING, 2)
    assert result is not None
    assert 'date: "[[01-Mar-2026]]"' in result
    assert "content" in result


# ---------------------------------------------------------------------------
# File-based get_rating / update_rating
# ---------------------------------------------------------------------------


def test_get_rating_from_file(tmp_path):
    note = tmp_path / "test.md"
    note.write_text(FRONTMATTER_WITH_RATING, encoding="utf-8")
    assert get_rating(note) == 7


def test_get_rating_file_not_found(tmp_path):
    assert get_rating(tmp_path / "nonexistent.md") is None


def test_update_rating_modifies_file(tmp_path):
    note = tmp_path / "test.md"
    note.write_text(FRONTMATTER_WITH_RATING, encoding="utf-8")
    assert update_rating(note, 4) is True
    assert get_rating(note) == 4


def test_update_rating_file_not_found(tmp_path):
    assert update_rating(tmp_path / "nonexistent.md", 5) is False
