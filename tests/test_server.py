"""Tests for core/server.py — verifies each gRPC RPC mapping.

Strategy: patch the underlying core functions and call the servicer methods
directly (no actual gRPC transport needed).
"""

from unittest.mock import MagicMock, patch

from core.server import NotesServicer


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_context():
    """Return a minimal mock for grpc.ServicerContext."""
    ctx = MagicMock()
    return ctx


def _make_servicer():
    return NotesServicer()


# ---------------------------------------------------------------------------
# GetTodayDate
# ---------------------------------------------------------------------------


class TestGetTodayDate:
    def test_returns_date_without_md_extension(self):
        servicer = _make_servicer()
        with patch("core.server.get_today_filename", return_value="01-Mar-2026.md"):
            response = servicer.GetTodayDate(MagicMock(), _make_context())
        assert response.date == "01-Mar-2026"

    def test_strips_md_suffix(self):
        servicer = _make_servicer()
        with patch("core.server.get_today_filename", return_value="15-Dec-2025.md"):
            response = servicer.GetTodayDate(MagicMock(), _make_context())
        assert not response.date.endswith(".md")


# ---------------------------------------------------------------------------
# GetExistingDates
# ---------------------------------------------------------------------------


class TestGetExistingDates:
    def test_returns_all_dates_from_calendar_ops(self):
        servicer = _make_servicer()
        dates = {"01-Mar-2026", "28-Feb-2026"}
        with patch("core.server.get_existing_dates", return_value=dates):
            response = servicer.GetExistingDates(MagicMock(), _make_context())
        assert set(response.dates) == dates

    def test_returns_empty_list_when_no_notes(self):
        servicer = _make_servicer()
        with patch("core.server.get_existing_dates", return_value=set()):
            response = servicer.GetExistingDates(MagicMock(), _make_context())
        assert list(response.dates) == []


# ---------------------------------------------------------------------------
# EnsureNote
# ---------------------------------------------------------------------------


class TestEnsureNote:
    def test_creates_note_when_missing(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.create_daily_note_from_template") as mock_create,
        ):
            response = servicer.EnsureNote(request, _make_context())

        mock_create.assert_called_once()
        assert response.success is True

    def test_does_not_create_note_when_exists(self, tmp_path):
        servicer = _make_servicer()
        note_file = tmp_path / "01-Mar-2026.md"
        note_file.write_text("existing content", encoding="utf-8")

        request = MagicMock()
        request.date = "01-Mar-2026"

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.create_daily_note_from_template") as mock_create,
        ):
            response = servicer.EnsureNote(request, _make_context())

        mock_create.assert_not_called()
        assert response.success is True


# ---------------------------------------------------------------------------
# GetNote
# ---------------------------------------------------------------------------


class TestGetNote:
    def test_returns_content_when_note_exists(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with patch("core.server.read_note", return_value="note content"):
            response = servicer.GetNote(request, _make_context())

        assert response.content == "note content"

    def test_sets_not_found_when_note_missing(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        ctx = _make_context()

        with patch("core.server.read_note", return_value=None):
            servicer.GetNote(request, ctx)

        ctx.set_code.assert_called_once()
        ctx.set_details.assert_called_once()


# ---------------------------------------------------------------------------
# GetRating
# ---------------------------------------------------------------------------


class TestGetRating:
    def test_returns_rating_when_present(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with (
            patch("core.server.read_note", return_value="content"),
            patch("core.server.get_rating_impl", return_value=8),
        ):
            response = servicer.GetRating(request, _make_context())

        assert response.has_rating is True
        assert response.rating == 8

    def test_returns_no_rating_when_absent(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with (
            patch("core.server.read_note", return_value="content"),
            patch("core.server.get_rating_impl", return_value=None),
        ):
            response = servicer.GetRating(request, _make_context())

        assert response.has_rating is False

    def test_returns_no_rating_when_note_missing(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with patch("core.server.read_note", return_value=None):
            response = servicer.GetRating(request, _make_context())

        assert response.has_rating is False


# ---------------------------------------------------------------------------
# UpdateRating
# ---------------------------------------------------------------------------


class TestUpdateRating:
    def test_delegates_to_update_rating_and_returns_success(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        request.rating = 7

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.update_rating", return_value=True) as mock_update,
        ):
            response = servicer.UpdateRating(request, _make_context())

        mock_update.assert_called_once_with(tmp_path / "01-Mar-2026.md", 7)
        assert response.success is True

    def test_propagates_failure_from_update_rating(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        request.rating = 7

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.update_rating", return_value=False),
        ):
            response = servicer.UpdateRating(request, _make_context())

        assert response.success is False


# ---------------------------------------------------------------------------
# GetTasks
# ---------------------------------------------------------------------------


class TestGetTasks:
    def test_returns_parsed_tasks(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        raw_tasks = [
            {"text": "Task 1", "completed": False, "index": 0, "line_number": 5},
            {"text": "Task 2", "completed": True, "index": 1, "line_number": 6},
        ]

        with (
            patch("core.server.read_note", return_value="content"),
            patch("core.server.parse_tasks", return_value=raw_tasks),
        ):
            response = servicer.GetTasks(request, _make_context())

        assert len(response.tasks) == 2
        assert response.tasks[0].text == "Task 1"
        assert response.tasks[0].completed is False
        assert response.tasks[1].completed is True

    def test_returns_empty_list_when_note_missing(self):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"

        with patch("core.server.read_note", return_value=None):
            response = servicer.GetTasks(request, _make_context())

        assert list(response.tasks) == []


# ---------------------------------------------------------------------------
# ToggleTask
# ---------------------------------------------------------------------------


class TestToggleTask:
    def test_delegates_to_toggle_task(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        request.task_index = 2

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.toggle_task", return_value=True) as mock_toggle,
        ):
            response = servicer.ToggleTask(request, _make_context())

        mock_toggle.assert_called_once_with(tmp_path / "01-Mar-2026.md", 2)
        assert response.success is True


# ---------------------------------------------------------------------------
# AddTask
# ---------------------------------------------------------------------------


class TestAddTask:
    def test_delegates_to_add_task(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        request.task_text = "New task"

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.add_task", return_value=True) as mock_add,
        ):
            response = servicer.AddTask(request, _make_context())

        mock_add.assert_called_once_with(tmp_path / "01-Mar-2026.md", "New task")
        assert response.success is True


# ---------------------------------------------------------------------------
# AppendToNote
# ---------------------------------------------------------------------------


class TestAppendToNote:
    def test_appends_text_to_existing_note(self, tmp_path):
        servicer = _make_servicer()
        note_file = tmp_path / "01-Mar-2026.md"
        note_file.write_text("existing\n", encoding="utf-8")

        request = MagicMock()
        request.date = "01-Mar-2026"
        request.text = "new line"

        with patch("core.server.DAILY_NOTES_DIR", tmp_path):
            response = servicer.AppendToNote(request, _make_context())

        assert response.success is True
        content = note_file.read_text(encoding="utf-8")
        assert "new line\n" in content

    def test_creates_note_then_appends_when_missing(self, tmp_path):
        servicer = _make_servicer()
        request = MagicMock()
        request.date = "01-Mar-2026"
        request.text = "first line"

        with (
            patch("core.server.DAILY_NOTES_DIR", tmp_path),
            patch("core.server.create_daily_note_from_template") as mock_create,
        ):
            # create_daily_note_from_template is called but we still need the file
            # to exist for the open() call — simulate by creating it in the mock
            def _side_effect(filepath, date):
                filepath.write_text("", encoding="utf-8")

            mock_create.side_effect = _side_effect
            response = servicer.AppendToNote(request, _make_context())

        assert response.success is True
        mock_create.assert_called_once()
