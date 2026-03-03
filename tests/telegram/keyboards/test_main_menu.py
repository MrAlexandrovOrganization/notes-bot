"""Tests for frontends/telegram/keyboards/main_menu.py."""

from telegram import InlineKeyboardMarkup

from frontends.telegram.keyboards.main_menu import get_main_menu_keyboard


class TestGetMainMenuKeyboard:
    def _get_all_buttons(self, markup: InlineKeyboardMarkup):
        return [btn for row in markup.inline_keyboard for btn in row]

    def test_returns_inline_keyboard_markup(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        assert isinstance(result, InlineKeyboardMarkup)

    def test_has_exactly_five_buttons(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        assert len(buttons) == 5

    def test_has_rating_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        texts = [b.text for b in buttons]
        assert "📊 Оценка" in texts

    def test_has_tasks_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        texts = [b.text for b in buttons]
        assert "✅ Задачи" in texts

    def test_has_note_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        texts = [b.text for b in buttons]
        assert "📝 Заметка" in texts

    def test_has_calendar_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        texts = [b.text for b in buttons]
        assert "📅 Календарь" in texts

    def test_has_notifications_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        texts = [b.text for b in buttons]
        assert "🔔 Уведомления" in texts

    def test_rating_callback_data(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        cb_data = {b.text: b.callback_data for b in buttons}
        assert cb_data["📊 Оценка"] == "menu:rating"

    def test_tasks_callback_data(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        cb_data = {b.text: b.callback_data for b in buttons}
        assert cb_data["✅ Задачи"] == "menu:tasks"

    def test_note_callback_data(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        cb_data = {b.text: b.callback_data for b in buttons}
        assert cb_data["📝 Заметка"] == "menu:note"

    def test_calendar_callback_data(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        cb_data = {b.text: b.callback_data for b in buttons}
        assert cb_data["📅 Календарь"] == "menu:calendar"

    def test_notifications_callback_data(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        buttons = self._get_all_buttons(result)
        cb_data = {b.text: b.callback_data for b in buttons}
        assert cb_data["🔔 Уведомления"] == "menu:notifications"

    def test_has_three_rows(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        assert len(result.inline_keyboard) == 3

    def test_first_row_has_two_buttons(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        assert len(result.inline_keyboard[0]) == 2

    def test_last_row_has_one_button(self):
        result = get_main_menu_keyboard("04-Mar-2026")
        assert len(result.inline_keyboard[2]) == 1
