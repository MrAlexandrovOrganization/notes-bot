"""Calendar keyboard for the Telegram bot."""

import calendar
from typing import Set
from telegram import InlineKeyboardButton, InlineKeyboardMarkup


# Russian month names
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
    year: int, month: int, active_date: str, existing_dates: Set[str]
) -> InlineKeyboardMarkup:
    """
    Generate calendar keyboard for date selection.

    Args:
        year: Year to display
        month: Month to display (1-12)
        active_date: Currently active date in DD-MMM-YYYY format
        existing_dates: Set of dates with existing notes in DD-MMM-YYYY format

    Returns:
        InlineKeyboardMarkup with calendar
    """
    keyboard: list[list[InlineKeyboardButton]] = []

    # Header with month/year and navigation
    month_name = MONTH_NAMES[month]
    header_text = f"◀ {month_name} {year} ▶"
    keyboard.append(
        [
            InlineKeyboardButton("◀", callback_data="cal:prev"),
            InlineKeyboardButton(header_text, callback_data="cal:noop"),
            InlineKeyboardButton("▶", callback_data="cal:next"),
        ]
    )

    # Days of week header
    weekdays = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"]
    keyboard.append(
        [InlineKeyboardButton(day, callback_data="cal:noop") for day in weekdays]
    )

    # Get calendar for the month
    month_calendar = calendar.monthcalendar(year, month)

    # Add day buttons
    for week in month_calendar:
        week_buttons: list[InlineKeyboardButton] = []
        for day in week:
            if day == 0:
                # Empty cell
                week_buttons.append(InlineKeyboardButton(" ", callback_data="cal:noop"))
            else:
                # Format date as DD-MMM-YYYY
                date_str = f"{day:02d}-{calendar.month_abbr[month]}-{year}"

                # Determine button text
                if date_str == active_date:
                    # Active date - show in brackets
                    button_text = f"[{day}]"
                elif date_str in existing_dates:
                    # Date with notes - show in bold (using special formatting)
                    button_text = f"*{day}*"
                else:
                    # Regular date
                    button_text = str(day)

                callback_data = f"cal:select:{date_str}"
                week_buttons.append(
                    InlineKeyboardButton(button_text, callback_data=callback_data)
                )

        keyboard.append(week_buttons)

    # Bottom navigation buttons
    keyboard.append(
        [
            InlineKeyboardButton("📅 Сегодня", callback_data="cal:today"),
            InlineKeyboardButton("◀ Назад", callback_data="cal:back"),
        ]
    )

    return InlineKeyboardMarkup(keyboard)
