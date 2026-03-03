"""Tests for frontends/telegram/keyboards/calendar.py."""

from telegram import InlineKeyboardMarkup

from frontends.telegram.keyboards.calendar import get_calendar_keyboard


def _all_buttons(markup: InlineKeyboardMarkup):
    return [btn for row in markup.inline_keyboard for btn in row]


class TestGetCalendarKeyboard:
    # March 2026 for a deterministic calendar layout
    YEAR = 2026
    MONTH = 3  # March
    ACTIVE_DATE = "04-Mar-2026"

    def _build(self, active_date=None, existing_dates=None):
        return get_calendar_keyboard(
            self.YEAR,
            self.MONTH,
            active_date or self.ACTIVE_DATE,
            existing_dates or set(),
        )

    def test_returns_inline_keyboard_markup(self):
        assert isinstance(self._build(), InlineKeyboardMarkup)

    def test_header_row_contains_month_name(self):
        result = self._build()
        # Row 0 is header: ◀  MonthName Year ▶
        header_row = result.inline_keyboard[0]
        center_btn = header_row[1]
        assert "Март" in center_btn.text
        assert "2026" in center_btn.text

    def test_header_prev_callback(self):
        result = self._build()
        prev_btn = result.inline_keyboard[0][0]
        assert prev_btn.callback_data == "cal:prev"

    def test_header_next_callback(self):
        result = self._build()
        next_btn = result.inline_keyboard[0][2]
        assert next_btn.callback_data == "cal:next"

    def test_weekday_row_has_seven_columns(self):
        result = self._build()
        weekday_row = result.inline_keyboard[1]
        assert len(weekday_row) == 7

    def test_weekday_row_starts_with_monday(self):
        result = self._build()
        weekday_row = result.inline_keyboard[1]
        assert weekday_row[0].text == "Пн"

    def test_weekday_row_ends_with_sunday(self):
        result = self._build()
        weekday_row = result.inline_keyboard[1]
        assert weekday_row[6].text == "Вс"

    def test_active_date_shown_in_brackets(self):
        result = self._build(active_date="04-Mar-2026")
        buttons = _all_buttons(result)
        active_btns = [b for b in buttons if b.text == "[4]"]
        assert len(active_btns) == 1

    def test_active_date_button_has_select_callback(self):
        result = self._build(active_date="04-Mar-2026")
        buttons = _all_buttons(result)
        active_btn = next(b for b in buttons if b.text == "[4]")
        assert active_btn.callback_data == "cal:select:04-Mar-2026"

    def test_date_with_existing_note_shown_with_marker(self):
        result = self._build(existing_dates={"10-Mar-2026"})
        buttons = _all_buttons(result)
        marked_btns = [b for b in buttons if b.text == "*10*"]
        assert len(marked_btns) == 1

    def test_regular_date_shown_as_plain_number(self):
        result = self._build(active_date="04-Mar-2026", existing_dates=set())
        buttons = _all_buttons(result)
        plain_btns = [b for b in buttons if b.text == "15"]
        assert len(plain_btns) == 1

    def test_today_button_callback(self):
        result = self._build()
        buttons = _all_buttons(result)
        today_btn = next(b for b in buttons if "Сегодня" in b.text)
        assert today_btn.callback_data == "cal:today"

    def test_back_button_present(self):
        result = self._build()
        buttons = _all_buttons(result)
        back_btns = [b for b in buttons if "Назад" in b.text]
        assert len(back_btns) == 1

    def test_back_button_callback(self):
        result = self._build()
        buttons = _all_buttons(result)
        back_btn = next(b for b in buttons if "Назад" in b.text)
        assert back_btn.callback_data == "cal:back"

    def test_empty_cells_have_noop_callback(self):
        # March 2026 starts on Sunday, so Mon-Sat of week 1 are empty
        result = self._build()
        noop_btns = [
            b
            for b in _all_buttons(result)
            if b.callback_data == "cal:noop" and b.text == " "
        ]
        assert len(noop_btns) > 0

    def test_day_buttons_have_select_callback(self):
        result = self._build(active_date="01-Jan-2000")
        buttons = _all_buttons(result)
        select_btns = [b for b in buttons if b.callback_data.startswith("cal:select:")]
        assert len(select_btns) == 31  # March has 31 days

    def test_different_month_renders_correct_name(self):
        result = get_calendar_keyboard(2026, 1, "01-Jan-2026", set())
        header_row = result.inline_keyboard[0]
        assert "Январь" in header_row[1].text
