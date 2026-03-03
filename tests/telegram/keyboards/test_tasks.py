"""Tests for frontends/telegram/keyboards/tasks.py."""

from telegram import InlineKeyboardMarkup

from frontends.telegram.keyboards.tasks import get_task_add_keyboard, get_tasks_keyboard


def _all_buttons(markup: InlineKeyboardMarkup):
    return [btn for row in markup.inline_keyboard for btn in row]


def _make_task(index: int, text: str = "Task", completed: bool = False):
    return {"index": index, "text": text, "completed": completed}


class TestGetTasksKeyboard:
    def test_returns_inline_keyboard_markup(self):
        result = get_tasks_keyboard([])
        assert isinstance(result, InlineKeyboardMarkup)

    def test_empty_task_list_has_add_and_back_buttons(self):
        result = get_tasks_keyboard([])
        buttons = _all_buttons(result)
        texts = [b.text for b in buttons]
        assert "➕ Добавить задачу" in texts
        assert "◀ Назад" in texts

    def test_empty_task_list_has_no_task_buttons(self):
        result = get_tasks_keyboard([])
        buttons = _all_buttons(result)
        task_buttons = [
            b for b in buttons if b.callback_data.startswith("task:toggle:")
        ]
        assert len(task_buttons) == 0

    def test_completed_task_shows_checkmark(self):
        tasks = [_make_task(0, "Done task", completed=True)]
        result = get_tasks_keyboard(tasks)
        buttons = _all_buttons(result)
        task_btn = next(b for b in buttons if "Done task" in b.text)
        assert task_btn.text.startswith("✅")

    def test_incomplete_task_shows_cross(self):
        tasks = [_make_task(0, "Todo task", completed=False)]
        result = get_tasks_keyboard(tasks)
        buttons = _all_buttons(result)
        task_btn = next(b for b in buttons if "Todo task" in b.text)
        assert task_btn.text.startswith("❌")

    def test_task_toggle_callback_data(self):
        tasks = [_make_task(3, "My task")]
        result = get_tasks_keyboard(tasks)
        buttons = _all_buttons(result)
        toggle_btn = next(
            b for b in buttons if b.callback_data.startswith("task:toggle:")
        )
        assert toggle_btn.callback_data == "task:toggle:3"

    def test_add_task_callback_data(self):
        result = get_tasks_keyboard([])
        buttons = _all_buttons(result)
        add_btn = next(b for b in buttons if "Добавить" in b.text)
        assert add_btn.callback_data == "task:add"

    def test_back_button_callback_data(self):
        result = get_tasks_keyboard([])
        buttons = _all_buttons(result)
        back_btn = next(b for b in buttons if "Назад" in b.text)
        assert back_btn.callback_data == "task:back"

    def test_five_tasks_no_pagination(self):
        tasks = [_make_task(i) for i in range(5)]
        result = get_tasks_keyboard(tasks, current_page=0)
        buttons = _all_buttons(result)
        page_btns = [b for b in buttons if b.callback_data.startswith("task:page:")]
        assert len(page_btns) == 0

    def test_six_tasks_adds_pagination(self):
        tasks = [_make_task(i) for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=0)
        buttons = _all_buttons(result)
        page_btns = [b for b in buttons if b.callback_data.startswith("task:page:")]
        assert len(page_btns) >= 1

    def test_first_page_no_prev_button(self):
        tasks = [_make_task(i) for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=0)
        buttons = _all_buttons(result)
        prev_btns = [
            b
            for b in buttons
            if b.text == "◀" and b.callback_data.startswith("task:page:")
        ]
        assert len(prev_btns) == 0

    def test_first_page_has_next_button(self):
        tasks = [_make_task(i) for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=0)
        buttons = _all_buttons(result)
        next_btns = [b for b in buttons if b.text == "▶"]
        assert len(next_btns) == 1
        assert next_btns[0].callback_data == "task:page:1"

    def test_second_page_has_prev_button(self):
        tasks = [_make_task(i) for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=1)
        buttons = _all_buttons(result)
        prev_btns = [b for b in buttons if b.text == "◀"]
        assert len(prev_btns) == 1
        assert prev_btns[0].callback_data == "task:page:0"

    def test_last_page_no_next_button(self):
        tasks = [_make_task(i) for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=1)
        buttons = _all_buttons(result)
        next_btns = [b for b in buttons if b.text == "▶"]
        assert len(next_btns) == 0

    def test_shows_only_current_page_tasks(self):
        tasks = [_make_task(i, f"Task {i}") for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=0, tasks_per_page=5)
        buttons = _all_buttons(result)
        task_btns = [b for b in buttons if b.callback_data.startswith("task:toggle:")]
        assert len(task_btns) == 5

    def test_second_page_shows_remaining_tasks(self):
        tasks = [_make_task(i, f"Task {i}") for i in range(6)]
        result = get_tasks_keyboard(tasks, current_page=1, tasks_per_page=5)
        buttons = _all_buttons(result)
        task_btns = [b for b in buttons if b.callback_data.startswith("task:toggle:")]
        assert len(task_btns) == 1


class TestGetTaskAddKeyboard:
    def test_returns_inline_keyboard_markup(self):
        result = get_task_add_keyboard()
        assert isinstance(result, InlineKeyboardMarkup)

    def test_has_single_cancel_button(self):
        result = get_task_add_keyboard()
        buttons = _all_buttons(result)
        assert len(buttons) == 1

    def test_cancel_button_callback_data(self):
        result = get_task_add_keyboard()
        buttons = _all_buttons(result)
        assert buttons[0].callback_data == "task:cancel"

    def test_cancel_button_has_cancel_text(self):
        result = get_task_add_keyboard()
        buttons = _all_buttons(result)
        assert "Отмена" in buttons[0].text
