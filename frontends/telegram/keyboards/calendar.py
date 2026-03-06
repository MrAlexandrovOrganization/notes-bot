"""Calendar keyboard for the Telegram bot."""

import calendar
from telegram import InlineKeyboardButton, InlineKeyboardMarkup


MONTH_NAMES = {
    1: "Январь",
    2: "Февраль",
    3: "Март",
    4: "Апрель",
    5: "Май",
    6: "Июнь",
    7: "Июль",
    8: "Август",
    9: "Сентябрь",
    10: "Октябрь",
    11: "Ноябрь",
    12: "Декабрь",
}


def get_calendar_keyboard(
    year: int, month: int, active_date: str, existing_dates: set[str]
) -> InlineKeyboardMarkup:
    """
    Generate calendar keyboard for date selection.

    Args:
        year: Year to display
        month: Month to display (1-12)
        active_date: Currently active date in DD-MMM-YYYY format
        existing_dates: Set of dates with existing notes in DD-MMM-YYYY format
    """
    keyboard: list[list[InlineKeyboardButton]] = []

    month_name = MONTH_NAMES[month]
    keyboard.append(
        [
            InlineKeyboardButton("◀", callback_data="cal:prev"),
            InlineKeyboardButton(f"◀ {month_name} {year} ▶", callback_data="cal:noop"),
            InlineKeyboardButton("▶", callback_data="cal:next"),
        ]
    )

    keyboard.append(
        [
            InlineKeyboardButton(day, callback_data="cal:noop")
            for day in ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"]
        ]
    )

    for week in calendar.monthcalendar(year, month):
        week_buttons: list[InlineKeyboardButton] = []
        for day in week:
            if day == 0:
                week_buttons.append(InlineKeyboardButton(" ", callback_data="cal:noop"))
            else:
                date_str = f"{day:02d}-{calendar.month_abbr[month]}-{year}"
                if date_str == active_date:
                    button_text = f"[{day}]"
                elif date_str in existing_dates:
                    button_text = f"*{day}*"
                else:
                    button_text = str(day)
                week_buttons.append(
                    InlineKeyboardButton(
                        button_text, callback_data=f"cal:select:{date_str}"
                    )
                )
        keyboard.append(week_buttons)

    keyboard.append(
        [
            InlineKeyboardButton("📅 Сегодня", callback_data="cal:today"),
            InlineKeyboardButton("◀ Назад", callback_data="cal:back"),
        ]
    )

    return InlineKeyboardMarkup(keyboard)
