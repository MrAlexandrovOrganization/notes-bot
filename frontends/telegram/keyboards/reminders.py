"""Reminders keyboards for the Telegram bot."""

import calendar
from datetime import datetime, timezone, timedelta
from typing import Any, Dict, List

from telegram import InlineKeyboardButton, InlineKeyboardMarkup

from ..config import TIMEZONE_OFFSET_HOURS

_MONTH_NAMES = {
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

_SCHEDULE_TYPE_LABELS = {
    "daily": "Каждый день",
    "weekly": "По дням недели",
    "monthly": "Каждый месяц",
    "yearly": "Каждый год",
    "once": "Один раз",
    "custom_days": "Каждые N дней",
}


def get_reminders_list_keyboard(
    reminders: List[Dict[str, Any]],
    page: int = 0,
    per_page: int = 5,
) -> InlineKeyboardMarkup:
    total = len(reminders)
    total_pages = max(1, (total + per_page - 1) // per_page)
    page = max(0, min(page, total_pages - 1))
    start = page * per_page
    end = min(start + per_page, total)

    keyboard = []
    for r in reminders[start:end]:
        label = r["title"][:30]
        keyboard.append(
            [
                InlineKeyboardButton(f"🔔 {label}", callback_data="reminder:noop"),
                InlineKeyboardButton("🗑", callback_data=f"reminder:delete:{r['id']}"),
            ]
        )

    if total_pages > 1:
        nav: List[InlineKeyboardButton] = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀", callback_data=f"reminder:page:{page - 1}"))
        nav.append(
            InlineKeyboardButton(f"{page + 1}/{total_pages}", callback_data="reminder:noop")
        )
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("▶", callback_data=f"reminder:page:{page + 1}"))
        keyboard.append(nav)

    keyboard.append(
        [InlineKeyboardButton("➕ Создать", callback_data="reminder:create")]
    )
    keyboard.append([InlineKeyboardButton("◀ Назад", callback_data="reminder:back")])
    return InlineKeyboardMarkup(keyboard)


def get_task_confirm_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        [
            [
                InlineKeyboardButton("✅ Да, создавать задачу", callback_data="reminder:task_confirm:yes"),
                InlineKeyboardButton("❌ Нет", callback_data="reminder:task_confirm:no"),
            ],
            [InlineKeyboardButton("❌ Отмена", callback_data="reminder:cancel")],
        ]
    )


def get_schedule_type_keyboard() -> InlineKeyboardMarkup:
    keyboard = [
        [
            InlineKeyboardButton("Каждый день", callback_data="reminder:type:daily"),
            InlineKeyboardButton(
                "По дням недели", callback_data="reminder:type:weekly"
            ),
        ],
        [
            InlineKeyboardButton("Каждый месяц", callback_data="reminder:type:monthly"),
            InlineKeyboardButton("Каждый год", callback_data="reminder:type:yearly"),
        ],
        [
            InlineKeyboardButton("Один раз", callback_data="reminder:type:once"),
            InlineKeyboardButton(
                "Каждые N дней", callback_data="reminder:type:custom_days"
            ),
        ],
        [InlineKeyboardButton("❌ Отмена", callback_data="reminder:cancel")],
    ]
    return InlineKeyboardMarkup(keyboard)


def get_reminder_cancel_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        [[InlineKeyboardButton("❌ Отмена", callback_data="reminder:cancel")]]
    )


def get_reminder_calendar_keyboard(
    year: int, month: int, context_name: str
) -> InlineKeyboardMarkup:
    """Calendar keyboard for picking a date in reminder flows.

    context_name: "once" | "yr" (yearly month+day) | "pp" (postpone)
    """
    local_now = datetime.now(timezone.utc) + timedelta(hours=TIMEZONE_OFFSET_HOURS)
    today = local_now.date()

    keyboard: list[list[InlineKeyboardButton]] = []

    # Header with navigation
    month_name = _MONTH_NAMES[month]
    keyboard.append(
        [
            InlineKeyboardButton(
                "◀", callback_data=f"reminder:cal:prev:{context_name}"
            ),
            InlineKeyboardButton(f"{month_name} {year}", callback_data="reminder:noop"),
            InlineKeyboardButton(
                "▶", callback_data=f"reminder:cal:next:{context_name}"
            ),
        ]
    )

    # Weekday headers
    keyboard.append(
        [
            InlineKeyboardButton(d, callback_data="reminder:noop")
            for d in ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"]
        ]
    )

    import datetime as dt_module

    for week in calendar.monthcalendar(year, month):
        row: list[InlineKeyboardButton] = []
        for day in week:
            if day == 0:
                row.append(InlineKeyboardButton(" ", callback_data="reminder:noop"))
                continue
            cell_date = dt_module.date(year, month, day)
            if cell_date < today:
                row.append(InlineKeyboardButton(" ", callback_data="reminder:noop"))
            else:
                date_str = cell_date.strftime("%Y-%m-%d")
                label = f"[{day}]" if cell_date == today else str(day)
                row.append(
                    InlineKeyboardButton(
                        label,
                        callback_data=f"reminder:cal:select:{date_str}:{context_name}",
                    )
                )
        keyboard.append(row)

    keyboard.append(
        [
            InlineKeyboardButton(
                "📅 Сегодня", callback_data=f"reminder:cal:today:{context_name}"
            ),
            InlineKeyboardButton("❌ Отмена", callback_data="reminder:cancel"),
        ]
    )

    return InlineKeyboardMarkup(keyboard)
