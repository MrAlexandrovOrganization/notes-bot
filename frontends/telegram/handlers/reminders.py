"""Reminder handlers for the Telegram bot."""

import json
import logging
from datetime import datetime, timezone, timedelta
from typing import Optional

import grpc
from telegram import CallbackQuery, InlineKeyboardMarkup, Update

from ..config import TIMEZONE_OFFSET_HOURS
from ..notifications_client import notifications_client
from ..states import state_manager
from ..states.context import UserState
from ..keyboards.main_menu import get_main_menu_keyboard
from ..keyboards.reminders import (
    get_reminders_list_keyboard,
    get_schedule_type_keyboard,
    get_reminder_cancel_keyboard,
    get_reminder_calendar_keyboard,
    get_task_confirm_keyboard,
)
from ..middleware import reply_message
from ..utils import escape_markdown_v2

logger = logging.getLogger(__name__)


def _local_now():
    return datetime.now(timezone.utc) + timedelta(hours=TIMEZONE_OFFSET_HOURS)


def _format_local_time(utc_str: str) -> str:
    """Convert a UTC ISO string to a local datetime string for display."""
    if not utc_str:
        return "—"
    try:
        s = utc_str.replace("Z", "+00:00")
        if "+" not in s[10:] and s[-6] not in ("+", "-"):
            s += "+00:00"
        dt = datetime.fromisoformat(s)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        local_dt = dt.astimezone(timezone(timedelta(hours=TIMEZONE_OFFSET_HOURS)))
        return local_dt.strftime("%d.%m.%Y %H:%M")
    except Exception:
        return utc_str[:16]


def _schedule_label(schedule_type: str) -> str:
    return {
        "daily": "каждый день",
        "weekly": "по дням недели",
        "monthly": "каждый месяц",
        "yearly": "каждый год",
        "once": "один раз",
        "custom_days": "каждые N дней",
    }.get(schedule_type, schedule_type)


def _cal_month_year(user_id: int) -> tuple[int, int]:
    """Return the currently displayed reminder-calendar month and year."""
    ctx = state_manager.get_context(user_id)
    now = _local_now()
    month = ctx.reminder_cal_month or now.month
    year = ctx.reminder_cal_year or now.year
    return month, year


def _reminder_list_text(reminders: list, page: int = 0) -> str:
    if not reminders:
        return "🔔 Уведомления:\n\nНапоминаний пока нет\\."
    per_page = 5
    start = page * per_page
    end = min(start + per_page, len(reminders))
    lines = [
        f"• {escape_markdown_v2(r['title'])} "
        f"\\({escape_markdown_v2(_schedule_label(r['schedule_type']))}\\) "
        f"— {escape_markdown_v2(_format_local_time(r['next_fire_at']))}"
        for r in reminders[start:end]
    ]
    return "🔔 Уведомления:\n\n" + "\n".join(lines)


async def _change_state_to_create_time(
    user_id: int,
    draft,
    update_or_query: Update | CallbackQuery,
    reply_markup: Optional[InlineKeyboardMarkup],
    text: str = "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):",
):
    if draft is not None:
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_TIME, reminder_draft=draft
        )
    await reply_message(
        update_or_query=update_or_query, text=text, keyboard=reply_markup
    )


async def _change_state_to_task_confirm(
    user_id: int,
    draft,
    update_or_query: Update | CallbackQuery,
) -> None:
    state_manager.update_context(
        user_id, state=UserState.REMINDER_CREATE_TASK_CONFIRM, reminder_draft=draft
    )
    await reply_message(
        update_or_query=update_or_query,
        text="➕ Создавать задачу в заметке при срабатывании напоминания?",
        keyboard=get_task_confirm_keyboard(),
    )


# ── List & create ──────────────────────────────────────────────────────────────


async def handle_menu_notifications(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    state_manager.update_context(user_id, state=UserState.REMINDER_LIST)
    reminders = notifications_client.list_reminders(user_id)
    page = ctx.reminder_list_page
    keyboard = get_reminders_list_keyboard(reminders, page=page)
    await reply_message(
        update_or_query=query,
        text=_reminder_list_text(reminders, page),
        keyboard=keyboard,
    )
    logger.info(f"User {user_id} opened reminders list")


async def handle_reminder_page(query: CallbackQuery, user_id: int, page: int) -> None:
    state_manager.update_context(user_id, reminder_list_page=page)
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders, page=page)
    try:
        await reply_message(
            update_or_query=query,
            text=_reminder_list_text(reminders, page),
            keyboard=keyboard,
        )
    except Exception as e:
        if "Message is not modified" not in str(e):
            raise
    logger.info(f"User {user_id} navigated to reminders page {page}")


async def handle_reminder_create(query: CallbackQuery, user_id: int) -> None:
    now = _local_now()
    state_manager.update_context(
        user_id,
        state=UserState.REMINDER_CREATE_TITLE,
        reminder_draft={},
        reminder_cal_month=now.month,
        reminder_cal_year=now.year,
    )
    await reply_message(
        update_or_query=query,
        text="🔔 Введите название напоминания:",
        keyboard=get_reminder_cancel_keyboard(),
    )
    logger.info(f"User {user_id} started reminder creation")


async def handle_reminder_title_input(update: Update, user_id: int, text: str) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    draft["title"] = text
    state_manager.update_context(
        user_id, state=UserState.REMINDER_CREATE_SCHEDULE_TYPE, reminder_draft=draft
    )
    await reply_message(
        update_or_query=update,
        text=f"Название: *{escape_markdown_v2(text)}*\n\nВыберите тип расписания:",
        keyboard=get_schedule_type_keyboard(),
    )
    logger.info(f"User {user_id} set reminder title: {text}")


async def handle_reminder_type_select(
    query: CallbackQuery, user_id: int, schedule_type: str
) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    draft["schedule_type"] = schedule_type
    cancel_kb = get_reminder_cancel_keyboard()
    now = _local_now()
    month, year = now.month, now.year

    if schedule_type == "weekly":
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_DAY, reminder_draft=draft
        )
        await reply_message(
            update_or_query=query,
            text="Введите дни недели через запятую \\(0\\=Пн, 1\\=Вт, …, 6\\=Вс\\)\\.\nПример: `0,2,4`",
            keyboard=cancel_kb,
        )

    elif schedule_type == "monthly":
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_DAY, reminder_draft=draft
        )
        await reply_message(
            update_or_query=query,
            text="Введите число месяца \\(1–31\\):",
            keyboard=cancel_kb,
        )

    elif schedule_type == "custom_days":
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_INTERVAL, reminder_draft=draft
        )
        await reply_message(
            update_or_query=query,
            text="Введите интервал в днях \\(например `3`\\):",
            keyboard=cancel_kb,
        )

    elif schedule_type in ("once", "yearly"):
        state_manager.update_context(
            user_id,
            state=UserState.REMINDER_CREATE_DATE,
            reminder_draft=draft,
            reminder_cal_month=month,
            reminder_cal_year=year,
        )
        context_name = "once" if schedule_type == "once" else "yr"
        await reply_message(
            update_or_query=query,
            text="📅 Выберите дату:"
            if schedule_type == "once"
            else "📅 Выберите день года:",
            keyboard=get_reminder_calendar_keyboard(year, month, context_name),
        )

    else:
        # daily — ask about task creation first
        await _change_state_to_task_confirm(
            user_id=user_id,
            draft=draft,
            update_or_query=query,
        )
    logger.info(f"User {user_id} selected schedule type: {schedule_type}")


# ── Calendar navigation ────────────────────────────────────────────────────────


def _cal_prompt(context_name: str) -> str:
    return "📅 Выберите дату переноса:" if context_name == "pp" else "📅 Выберите дату:"


async def _edit_calendar(
    query: CallbackQuery, year: int, month: int, context_name: str
) -> None:
    try:
        await reply_message(
            update_or_query=query,
            text=_cal_prompt(context_name),
            keyboard=get_reminder_calendar_keyboard(year, month, context_name),
        )
    except Exception as e:
        if "Message is not modified" not in str(e):
            raise


async def handle_reminder_cal_prev(
    query: CallbackQuery, user_id: int, context_name: str
) -> None:
    month, year = _cal_month_year(user_id)
    if month == 1:
        month, year = 12, year - 1
    else:
        month -= 1
    state_manager.update_context(
        user_id, reminder_cal_month=month, reminder_cal_year=year
    )
    await _edit_calendar(query, year, month, context_name)


async def handle_reminder_cal_next(
    query: CallbackQuery, user_id: int, context_name: str
) -> None:
    month, year = _cal_month_year(user_id)
    if month == 12:
        month, year = 1, year + 1
    else:
        month += 1
    state_manager.update_context(
        user_id, reminder_cal_month=month, reminder_cal_year=year
    )
    await _edit_calendar(query, year, month, context_name)


async def handle_reminder_cal_today(
    query: CallbackQuery, user_id: int, context_name: str
) -> None:
    now = _local_now()
    month, year = now.month, now.year
    state_manager.update_context(
        user_id, reminder_cal_month=month, reminder_cal_year=year
    )
    await _edit_calendar(query, year, month, context_name)


async def handle_reminder_cal_select(
    query: CallbackQuery, user_id: int, date_str: str, context_name: str
) -> None:
    """Called when user picks a date from the reminder calendar."""
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    cancel_kb = get_reminder_cancel_keyboard()

    if context_name == "pp":
        reminder_id = ctx.pending_postpone_reminder_id
        if reminder_id:
            notifications_client.postpone_reminder(
                reminder_id=reminder_id, user_id=user_id, target_date=date_str
            )
        state_manager.update_context(
            user_id, state=UserState.IDLE, pending_postpone_reminder_id=None
        )
        await reply_message(
            update_or_query=query,
            text=f"✅ Напоминание перенесено на {escape_markdown_v2(date_str)}\\.",
        )
        logger.info(f"User {user_id} postponed reminder {reminder_id} to {date_str}")
        return

    if context_name == "yr":
        try:
            dt = datetime.strptime(date_str, "%Y-%m-%d")
            draft["month"] = dt.month
            draft["day"] = dt.day
        except ValueError:
            await reply_message(
                update_or_query=query,
                text="❌ Неверная дата\\.",
                keyboard=cancel_kb,
            )
            return
    else:
        # once: use full date
        draft["date"] = date_str

    await _change_state_to_task_confirm(
        user_id=user_id,
        draft=draft,
        update_or_query=query,
    )
    logger.info(f"User {user_id} selected date {date_str} (context={context_name})")


# ── Text param input ───────────────────────────────────────────────────────────


async def handle_reminder_param_input(update: Update, user_id: int, text: str) -> None:
    ctx = state_manager.get_context(user_id)
    state = ctx.state
    draft = dict(ctx.reminder_draft)
    schedule_type = str(draft.get("schedule_type", ""))
    cancel_kb = get_reminder_cancel_keyboard()

    if state == UserState.REMINDER_CREATE_DAY:
        if schedule_type == "weekly":
            try:
                days = [int(d.strip()) for d in text.split(",")]
                if not all(0 <= d <= 6 for d in days):
                    raise ValueError
            except ValueError:
                await reply_message(
                    update_or_query=update,
                    text="❌ Введите числа от 0 до 6 через запятую\\.",
                    keyboard=cancel_kb,
                )
                return
            draft["days"] = days

        elif schedule_type == "monthly":
            try:
                day = int(text.strip())
                if not 1 <= day <= 31:
                    raise ValueError
            except ValueError:
                await reply_message(
                    update_or_query=update,
                    text="❌ Введите число от 1 до 31\\.",
                    keyboard=cancel_kb,
                )
                return
            draft["day_of_month"] = day

        await _change_state_to_task_confirm(
            user_id=user_id,
            draft=draft,
            update_or_query=update,
        )

    elif state == UserState.REMINDER_CREATE_INTERVAL:
        try:
            interval = int(text.strip())
            if interval < 1:
                raise ValueError
        except ValueError:
            await reply_message(
                update_or_query=update,
                text="❌ Введите положительное целое число\\.",
                keyboard=cancel_kb,
            )
            return
        draft["interval_days"] = interval
        await _change_state_to_task_confirm(
            user_id=user_id,
            draft=draft,
            update_or_query=update,
        )

    elif state == UserState.REMINDER_CREATE_TIME:
        try:
            h, m = text.strip().split(":")
            hour, minute = int(h), int(m)
            if not (0 <= hour <= 23 and 0 <= minute <= 59):
                raise ValueError
        except (ValueError, AttributeError):
            await _change_state_to_create_time(
                user_id=user_id,
                draft=None,
                update_or_query=update,
                reply_markup=cancel_kb,
                text="❌ Введите время в формате ЧЧ:ММ\\.",
            )
            return
        draft["hour"] = hour
        draft["minute"] = minute
        state_manager.update_context(user_id, reminder_draft=draft)
        await _finalize_reminder_creation(update, user_id)


async def _finalize_reminder_creation(
    update_or_query: Update | CallbackQuery, user_id: int
) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    title = str(draft.pop("title", "Напоминание"))
    schedule_type = str(draft.pop("schedule_type", "daily"))
    create_task = bool(draft.pop("create_task", False))
    draft["tz_offset"] = TIMEZONE_OFFSET_HOURS
    params_json = json.dumps(draft)

    try:
        result = notifications_client.create_reminder(
            user_id=user_id,
            title=title,
            schedule_type=schedule_type,
            schedule_params_json=params_json,
            create_task=create_task,
        )
    except grpc.RpcError as e:
        if e.code() == grpc.StatusCode.INVALID_ARGUMENT:
            # Date/time is in the past — ask to re-enter time without losing draft
            await _change_state_to_create_time(
                user_id=user_id,
                draft=None,
                update_or_query=update_or_query,
                reply_markup=get_reminder_cancel_keyboard(),
                text="❌ Выбранное время уже прошло\\.\n"
                "Введите другое время в формате `ЧЧ:ММ`:",
            )
            return
        raise

    state_manager.update_context(user_id, state=UserState.REMINDER_LIST)
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders)

    if result:
        next_fire = _format_local_time(result.get("next_fire_at", ""))
        task_note = " \\(задача будет создана\\)" if create_task else ""
        text = (
            f"✅ Напоминание создано\\!\n\n"
            f"*{escape_markdown_v2(title)}*{task_note}\n"
            f"Тип: {escape_markdown_v2(_schedule_label(schedule_type))}\n"
            f"Следующее: {escape_markdown_v2(next_fire)}"
        )
    else:
        text = "❌ Не удалось создать напоминание\\."

    await reply_message(update_or_query, text, keyboard)
    logger.info(f"User {user_id} created reminder: {title}")


async def handle_reminder_task_confirm(
    query: CallbackQuery, user_id: int, create_task: bool
) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    draft["create_task"] = create_task
    await _change_state_to_create_time(
        user_id=user_id,
        draft=draft,
        update_or_query=query,
        reply_markup=get_reminder_cancel_keyboard(),
    )


# ── Notification message actions ───────────────────────────────────────────────


async def handle_reminder_delete(
    query: CallbackQuery, user_id: int, reminder_id: int
) -> None:
    notifications_client.delete_reminder(reminder_id, user_id)
    state_manager.update_context(user_id, state=UserState.REMINDER_LIST)
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders)
    text = (
        "🔔 Уведомления:\n\nНапоминание удалено\\."
        if not reminders
        else "🔔 Уведомления:"
    )
    await reply_message(
        update_or_query=query,
        text=text,
        keyboard=keyboard,
    )
    logger.info(f"User {user_id} deleted reminder {reminder_id}")


async def handle_reminder_done(
    query: CallbackQuery,
    user_id: int,
    reminder_id: int,
    create_task_flag: int = 0,
    date_str: str = "",
) -> None:
    msg_text = str(query.message.text) or ""
    if create_task_flag and date_str:
        try:
            from ..grpc_client import core_client

            title = msg_text.removeprefix("🔔 Напоминание: ")
            tasks = core_client.get_tasks(date_str)
            for task in tasks:
                if task["text"] == title and not task["completed"]:
                    core_client.toggle_task(date_str, task["index"])
                    break
        except Exception as e:
            logger.error(f"Failed to toggle task on reminder done: {e}")

    original = escape_markdown_v2(msg_text)
    try:
        await reply_message(
            update_or_query=query,
            text=f"{original}\n\n✅ _Принято\\!_",
        )
    except Exception:
        pass
    logger.info(f"User {user_id} acknowledged reminder {reminder_id}")


async def _handle_postpone(
    query: CallbackQuery,
    user_id: int,
    reminder_id: int,
    amount: int,
    unit: str,
) -> None:
    result = notifications_client.postpone_reminder(
        reminder_id=reminder_id, user_id=user_id, **{f"postpone_{unit}": amount}
    )
    next_fire = _format_local_time(result.get("next_fire_at", "")) if result else ""
    next_fire_text = (
        f" \\(следующее: {escape_markdown_v2(next_fire)}\\)" if next_fire else ""
    )
    unit_label = "д" if unit == "days" else "ч"
    original = escape_markdown_v2(query.message.text or "")
    try:
        await reply_message(
            update_or_query=query,
            text=f"{original}\n\n⏰ _Перенесено на {amount} {unit_label}\\._"
            + next_fire_text,
        )
    except Exception:
        pass
    logger.info(f"User {user_id} postponed reminder {reminder_id} by {amount} {unit}")


async def handle_reminder_postpone_days(
    query: CallbackQuery, user_id: int, days: int, reminder_id: int
) -> None:
    await _handle_postpone(query, user_id, reminder_id, days, "days")


async def handle_reminder_postpone_hours(
    query: CallbackQuery, user_id: int, hours: int, reminder_id: int
) -> None:
    await _handle_postpone(query, user_id, reminder_id, hours, "hours")


async def handle_reminder_custom_date(
    query: CallbackQuery, user_id: int, reminder_id: int
) -> None:
    now = _local_now()
    state_manager.update_context(
        user_id,
        state=UserState.REMINDER_POSTPONE_DATE,
        pending_postpone_reminder_id=reminder_id,
        reminder_cal_month=now.month,
        reminder_cal_year=now.year,
    )
    await reply_message(
        update_or_query=query,
        text="📅 Выберите дату переноса:",
        keyboard=get_reminder_calendar_keyboard(now.year, now.month, "pp"),
    )
    logger.info(f"User {user_id} started custom postpone for reminder {reminder_id}")


# ── Navigation ─────────────────────────────────────────────────────────────────


async def handle_reminder_back(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    active_date = ctx.active_date
    state_manager.update_context(
        user_id,
        state=UserState.IDLE,
        reminder_draft={},
        pending_postpone_reminder_id=None,
    )
    await reply_message(
        update_or_query=query,
        text=f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\nВыберите действие:",
        keyboard=get_main_menu_keyboard(active_date),
    )
    logger.info(f"User {user_id} returned to main menu from reminders")


async def handle_reminder_cancel(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.REMINDER_LIST)
    reminders = notifications_client.list_reminders(user_id)
    await reply_message(
        update_or_query=query,
        text="🔔 Уведомления:"
        if reminders
        else "🔔 Уведомления:\n\nНапоминаний пока нет\\.",
        keyboard=get_reminders_list_keyboard(reminders),
    )
    logger.info(f"User {user_id} cancelled reminder action")
