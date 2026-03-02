"""Tests for core/features/tasks.py"""

from core.features.tasks import parse_tasks, toggle_task, add_task

# ---------------------------------------------------------------------------
# Sample note content
#
# Structure expected by the feature:
#   ---          <- delimiter 1
#   frontmatter
#   ---          <- delimiter 2
#   tasks section
#   ---          <- delimiter 3
#   rest of note
#
# split("---") produces at least 4 parts.
# ---------------------------------------------------------------------------

EMPTY_TASKS_NOTE = '---\ndate: "[[01-Mar-2026]]"\nОценка: 5\n---\n\n---\nSome text\n'

TASKS_NOTE = (
    "---\n"
    'date: "[[01-Mar-2026]]"\n'
    "Оценка: 5\n"
    "---\n"
    "- [ ] Task 1\n"
    "- [x] Task 2  [completion:: 2026-03-01]\n"
    "- [ ] Task 3\n"
    "---\n"
    "Some text\n"
)

INVALID_NOTE = "---\nno tasks section\n---\n"


# ---------------------------------------------------------------------------
# parse_tasks
# ---------------------------------------------------------------------------


def test_parse_tasks_empty_section():
    tasks = parse_tasks(EMPTY_TASKS_NOTE)
    assert tasks == []


def test_parse_tasks_count():
    tasks = parse_tasks(TASKS_NOTE)
    assert len(tasks) == 3


def test_parse_tasks_incomplete_task():
    tasks = parse_tasks(TASKS_NOTE)
    assert tasks[0]["text"] == "Task 1"
    assert tasks[0]["completed"] is False
    assert tasks[0]["index"] == 0


def test_parse_tasks_completed_task():
    tasks = parse_tasks(TASKS_NOTE)
    assert tasks[1]["text"] == "Task 2"
    assert tasks[1]["completed"] is True
    assert tasks[1]["index"] == 1


def test_parse_tasks_strips_completion_metadata():
    tasks = parse_tasks(TASKS_NOTE)
    assert "[completion::" not in tasks[1]["text"]


def test_parse_tasks_third_task():
    tasks = parse_tasks(TASKS_NOTE)
    assert tasks[2]["text"] == "Task 3"
    assert tasks[2]["completed"] is False


def test_parse_tasks_invalid_format():
    tasks = parse_tasks(INVALID_NOTE)
    assert tasks == []


def test_parse_tasks_line_numbers_are_positive():
    tasks = parse_tasks(TASKS_NOTE)
    for task in tasks:
        assert task["line_number"] > 0


# ---------------------------------------------------------------------------
# toggle_task
# ---------------------------------------------------------------------------


def test_toggle_incomplete_to_complete(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")

    assert toggle_task(note, 0) is True

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert tasks[0]["completed"] is True


def test_toggle_complete_to_incomplete(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")

    assert toggle_task(note, 1) is True

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert tasks[1]["completed"] is False


def test_toggle_adds_completion_date(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    toggle_task(note, 0)

    content = note.read_text(encoding="utf-8")
    assert "[completion::" in content


def test_toggle_removes_completion_metadata(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    toggle_task(note, 1)  # Task 2 was completed

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert "[completion::" not in tasks[1]["text"]


def test_toggle_preserves_other_tasks(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    toggle_task(note, 0)

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert len(tasks) == 3
    assert tasks[2]["text"] == "Task 3"
    assert tasks[2]["completed"] is False


def test_toggle_invalid_index(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    assert toggle_task(note, 99) is False


def test_toggle_file_not_found(tmp_path):
    assert toggle_task(tmp_path / "missing.md", 0) is False


def test_toggle_roundtrip(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")

    toggle_task(note, 0)  # incomplete -> complete
    toggle_task(note, 0)  # complete -> incomplete

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert tasks[0]["completed"] is False


# ---------------------------------------------------------------------------
# add_task
# ---------------------------------------------------------------------------


def test_add_task_increases_count(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")

    assert add_task(note, "New task") is True

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert len(tasks) == 4


def test_add_task_text(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    add_task(note, "My new task")

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    texts = [t["text"] for t in tasks]
    assert "My new task" in texts


def test_add_task_new_task_is_incomplete(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    add_task(note, "New task")

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    new_task = next(t for t in tasks if t["text"] == "New task")
    assert new_task["completed"] is False


def test_add_task_to_empty_section(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(EMPTY_TASKS_NOTE, encoding="utf-8")

    assert add_task(note, "First task") is True

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    assert len(tasks) == 1
    assert tasks[0]["text"] == "First task"


def test_add_task_file_not_found(tmp_path):
    assert add_task(tmp_path / "missing.md", "Task") is False


def test_add_task_preserves_existing_tasks(tmp_path):
    note = tmp_path / "note.md"
    note.write_text(TASKS_NOTE, encoding="utf-8")
    add_task(note, "Extra")

    content = note.read_text(encoding="utf-8")
    tasks = parse_tasks(content)
    existing = [t["text"] for t in tasks]
    assert "Task 1" in existing
    assert "Task 2" in existing
    assert "Task 3" in existing
