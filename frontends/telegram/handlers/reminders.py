"""Reminder handlers for the Telegram bot."""

import json
import logging
from datetime import datetime, timezone, timedelta

import grpc
from telegram import CallbackQuery, Update

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


# ── List & create ──────────────────────────────────────────────────────────────


async def handle_menu_notifications(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    state_manager.update_context(user_id, state=UserState.REMINDER_LIST)
    reminders = notifications_client.list_reminders(user_id)
    page = ctx.reminder_list_page
    keyboard = get_reminders_list_keyboard(reminders, page=page)

    if reminders:
        per_page = 5
        start = page * per_page
        end = min(start + per_page, len(reminders))
        lines = []
        for r in reminders[start:end]:
            next_fire = _format_local_time(r["next_fire_at"])
            lines.append(
                f"• {escape_markdown_v2(r['title'])} "
                f"\\({escape_markdown_v2(_schedule_label(r['schedule_type']))}\\) "
                f"— {escape_markdown_v2(next_fire)}"
            )
        text = "🔔 Уведомления:\n\n" + "\n".join(lines)
    else:
        text = "🔔 Уведомления:\n\nНапоминаний пока нет\\."

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")
    logger.info(f"User {user_id} opened reminders list")


async def handle_reminder_page(query: CallbackQuery, user_id: int, page: int) -> None:
    state_manager.update_context(user_id, reminder_list_page=page)
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders, page=page)

    per_page = 5
    start = page * per_page
    end = min(start + per_page, len(reminders))
    if reminders:
        lines = []
        for r in reminders[start:end]:
            next_fire = _format_local_time(r["next_fire_at"])
            lines.append(
                f"• {escape_markdown_v2(r['title'])} "
                f"\\({escape_markdown_v2(_schedule_label(r['schedule_type']))}\\) "
                f"— {escape_markdown_v2(next_fire)}"
            )
        text = "🔔 Уведомления:\n\n" + "\n".join(lines)
    else:
        text = "🔔 Уведомления:\n\nНапоминаний пока нет\\."

    try:
        await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")
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
    await query.edit_message_text(
        "🔔 Введите название напоминания:", reply_markup=get_reminder_cancel_keyboard()
    )
    logger.info(f"User {user_id} started reminder creation")


async def handle_reminder_title_input(update: Update, user_id: int, text: str) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    draft["title"] = text
    state_manager.update_context(
        user_id, state=UserState.REMINDER_CREATE_SCHEDULE_TYPE, reminder_draft=draft
    )
    await update.message.reply_text(
        f"Название: *{escape_markdown_v2(text)}*\n\nВыберите тип расписания:",
        reply_markup=get_schedule_type_keyboard(),
        parse_mode="MarkdownV2",
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
        await query.edit_message_text(
            "Введите дни недели через запятую \\(0\\=Пн, 1\\=Вт, …, 6\\=Вс\\)\\.\nПример: `0,2,4`",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    elif schedule_type == "monthly":
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_DAY, reminder_draft=draft
        )
        await query.edit_message_text(
            "Введите число месяца \\(1–31\\):",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    elif schedule_type == "custom_days":
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_INTERVAL, reminder_draft=draft
        )
        await query.edit_message_text(
            "Введите интервал в днях \\(например `3`\\):",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    elif schedule_type in ("once", "yearly"):
        # Show calendar picker
        state_manager.update_context(
            user_id,
            state=UserState.REMINDER_CREATE_DATE,
            reminder_draft=draft,
            reminder_cal_month=month,
            reminder_cal_year=year,
        )
        context_name = "once" if schedule_type == "once" else "yr"
        cal_kb = get_reminder_calendar_keyboard(year, month, context_name)
        prompt = (
            "📅 Выберите дату:" if schedule_type == "once" else "📅 Выберите день года:"
        )
        await query.edit_message_text(prompt, reply_markup=cal_kb)

    else:
        # daily — go straight to time
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_TIME, reminder_draft=draft
        )
        await query.edit_message_text(
            "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    logger.info(f"User {user_id} selected schedule type: {schedule_type}")


# ── Calendar navigation ────────────────────────────────────────────────────────


def _cal_prompt(context_name: str) -> str:
    return "📅 Выберите дату переноса:" if context_name == "pp" else "📅 Выберите дату:"


async def _edit_calendar(
    query: CallbackQuery, year: int, month: int, context_name: str
) -> None:
    cal_kb = get_reminder_calendar_keyboard(year, month, context_name)
    try:
        await query.edit_message_text(_cal_prompt(context_name), reply_markup=cal_kb)
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
        # Postpone flow
        reminder_id = ctx.pending_postpone_reminder_id
        if reminder_id:
            notifications_client.postpone_reminder(
                reminder_id=reminder_id, user_id=user_id, target_date=date_str
            )
        state_manager.update_context(
            user_id, state=UserState.IDLE, pending_postpone_reminder_id=None
        )
        await query.edit_message_text(
            f"✅ Напоминание перенесено на {escape_markdown_v2(date_str)}\\.",
            parse_mode="MarkdownV2",
        )
        logger.info(f"User {user_id} postponed reminder {reminder_id} to {date_str}")
        return

    if context_name == "yr":
        # yearly: extract month and day
        try:
            dt = datetime.strptime(date_str, "%Y-%m-%d")
            draft["month"] = dt.month
            draft["day"] = dt.day
        except ValueError:
            await query.edit_message_text(
                "❌ Неверная дата\\.", reply_markup=cancel_kb, parse_mode="MarkdownV2"
            )
            return
    else:
        # once: use full date
        draft["date"] = date_str

    state_manager.update_context(
        user_id, state=UserState.REMINDER_CREATE_TIME, reminder_draft=draft
    )
    await query.edit_message_text(
        "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):",
        reply_markup=cancel_kb,
        parse_mode="MarkdownV2",
    )
    logger.info(f"User {user_id} selected date {date_str} (context={context_name})")


# ── Text param input ───────────────────────────────────────────────────────────


async def handle_reminder_param_input(update: Update, user_id: int, text: str) -> None:
    ctx = state_manager.get_context(user_id)
    state = ctx.state
    draft = dict(ctx.reminder_draft)
    schedule_type = draft.get("schedule_type", "")
    cancel_kb = get_reminder_cancel_keyboard()

    if state == UserState.REMINDER_CREATE_DAY:
        if schedule_type == "weekly":
            try:
                days = [int(d.strip()) for d in text.split(",")]
                if not all(0 <= d <= 6 for d in days):
                    raise ValueError
            except ValueError:
                await update.message.reply_text(
                    "❌ Введите числа от 0 до 6 через запятую\\.",
                    reply_markup=cancel_kb,
                    parse_mode="MarkdownV2",
                )
                return
            draft["days"] = days

        elif schedule_type == "monthly":
            try:
                day = int(text.strip())
                if not 1 <= day <= 31:
                    raise ValueError
            except ValueError:
                await update.message.reply_text(
                    "❌ Введите число от 1 до 31\\.",
                    reply_markup=cancel_kb,
                    parse_mode="MarkdownV2",
                )
                return
            draft["day_of_month"] = day

        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_TIME, reminder_draft=draft
        )
        await update.message.reply_text(
            "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    elif state == UserState.REMINDER_CREATE_INTERVAL:
        try:
            interval = int(text.strip())
            if interval < 1:
                raise ValueError
        except ValueError:
            await update.message.reply_text(
                "❌ Введите положительное целое число\\.",
                reply_markup=cancel_kb,
                parse_mode="MarkdownV2",
            )
            return
        draft["interval_days"] = interval
        state_manager.update_context(
            user_id, state=UserState.REMINDER_CREATE_TIME, reminder_draft=draft
        )
        await update.message.reply_text(
            "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):",
            reply_markup=cancel_kb,
            parse_mode="MarkdownV2",
        )

    elif state == UserState.REMINDER_CREATE_TIME:
        try:
            h, m = text.strip().split(":")
            hour, minute = int(h), int(m)
            if not (0 <= hour <= 23 and 0 <= minute <= 59):
                raise ValueError
        except (ValueError, AttributeError):
            await update.message.reply_text(
                "❌ Введите время в формате ЧЧ:ММ\\.",
                reply_markup=cancel_kb,
                parse_mode="MarkdownV2",
            )
            return
        draft["hour"] = hour
        draft["minute"] = minute
        state_manager.update_context(
            user_id,
            state=UserState.REMINDER_CREATE_TASK_CONFIRM,
            reminder_draft=draft,
        )
        await update.message.reply_text(
            "➕ Создавать задачу в заметке при срабатывании напоминания?",
            reply_markup=get_task_confirm_keyboard(),
        )


async def _finalize_reminder_creation(update_or_query, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    title = draft.pop("title", "Напоминание")
    schedule_type = draft.pop("schedule_type", "daily")
    create_task = draft.pop("create_task", False)
    # Embed the client's timezone offset so the server uses the correct local time
    draft["tz_offset"] = TIMEZONE_OFFSET_HOURS
    params_json = json.dumps(draft)

    async def _reply(text, keyboard):
        if hasattr(update_or_query, "message") and update_or_query.message:
            await update_or_query.message.reply_text(
                text, reply_markup=keyboard, parse_mode="MarkdownV2"
            )
        else:
            await update_or_query.edit_message_text(
                text, reply_markup=keyboard, parse_mode="MarkdownV2"
            )

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
            state_manager.update_context(user_id, state=UserState.REMINDER_CREATE_TIME)
            await _reply(
                "❌ Выбранное время уже прошло\\.\n"
                "Введите другое время в формате `ЧЧ:ММ`:",
                get_reminder_cancel_keyboard(),
            )
            return
        raise

    state_manager.update_context(
        user_id, state=UserState.REMINDER_LIST, reminder_draft={}, reminder_list_page=0
    )
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

    await _reply(text, keyboard)
    logger.info(f"User {user_id} created reminder: {title}")


async def handle_reminder_task_confirm(
    query: CallbackQuery, user_id: int, create_task: bool
) -> None:
    ctx = state_manager.get_context(user_id)
    draft = dict(ctx.reminder_draft)
    draft["create_task"] = create_task
    state_manager.update_context(user_id, reminder_draft=draft)
    await _finalize_reminder_creation(query, user_id)


# ── Notification message actions ───────────────────────────────────────────────


async def handle_reminder_delete(
    query: CallbackQuery, user_id: int, reminder_id: int
) -> None:
    notifications_client.delete_reminder(reminder_id, user_id)
    state_manager.update_context(
        user_id, state=UserState.REMINDER_LIST, reminder_list_page=0
    )
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders)
    text = (
        "🔔 Уведомления:\n\nНапоминание удалено\\."
        if not reminders
        else "🔔 Уведомления:"
    )
    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")
    logger.info(f"User {user_id} deleted reminder {reminder_id}")


async def handle_reminder_done(
    query: CallbackQuery,
    user_id: int,
    reminder_id: int,
    create_task_flag: int = 0,
    date_str: str = "",
) -> None:
    if create_task_flag and date_str:
        # Find and toggle the task created for this reminder
        try:
            from ..grpc_client import core_client
            msg_text = query.message.text or ""
            title = msg_text.removeprefix("🔔 Напоминание: ")
            tasks = core_client.get_tasks(date_str)
            for task in tasks:
                if task["text"] == title and not task["completed"]:
                    core_client.toggle_task(date_str, task["index"])
                    break
        except Exception as e:
            logger.error(f"Failed to toggle task on reminder done: {e}")

    original = escape_markdown_v2(query.message.text or "")
    try:
        await query.edit_message_text(
            f"{original}\n\n✅ _Принято\\!_", parse_mode="MarkdownV2"
        )
    except Exception:
        pass
    logger.info(f"User {user_id} acknowledged reminder {reminder_id}")


async def handle_reminder_postpone_days(
    query: CallbackQuery, user_id: int, days: int, reminder_id: int
) -> None:
    result = notifications_client.postpone_reminder(
        reminder_id=reminder_id, user_id=user_id, postpone_days=days
    )
    next_fire = _format_local_time(result.get("next_fire_at", "")) if result else ""
    next_fire_text = f" \\(следующее: {escape_markdown_v2(next_fire)}\\)" if next_fire else ""
    original = escape_markdown_v2(query.message.text or "")
    try:
        await query.edit_message_text(
            f"{original}\n\n⏰ _Перенесено на {days} д\\._" + next_fire_text,
            parse_mode="MarkdownV2",
        )
    except Exception:
        pass
    logger.info(f"User {user_id} postponed reminder {reminder_id} by {days} days")


async def handle_reminder_postpone_hours(
    query: CallbackQuery, user_id: int, hours: int, reminder_id: int
) -> None:
    result = notifications_client.postpone_reminder(
        reminder_id=reminder_id, user_id=user_id, postpone_hours=hours
    )
    next_fire = _format_local_time(result.get("next_fire_at", "")) if result else ""
    next_fire_text = f" \\(следующее: {escape_markdown_v2(next_fire)}\\)" if next_fire else ""
    original = escape_markdown_v2(query.message.text or "")
    try:
        await query.edit_message_text(
            f"{original}\n\n⏰ _Перенесено на {hours} ч\\._" + next_fire_text,
            parse_mode="MarkdownV2",
        )
    except Exception:
        pass
    logger.info(f"User {user_id} postponed reminder {reminder_id} by {hours} hours")


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
    cal_kb = get_reminder_calendar_keyboard(now.year, now.month, "pp")
    # Edit the notification message to show the calendar inline
    await query.edit_message_text("📅 Выберите дату переноса:", reply_markup=cal_kb)
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
    keyboard = get_main_menu_keyboard(active_date)
    text = f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\nВыберите действие:"
    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")
    logger.info(f"User {user_id} returned to main menu from reminders")


async def handle_reminder_cancel(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(
        user_id,
        state=UserState.REMINDER_LIST,
        reminder_draft={},
        pending_postpone_reminder_id=None,
        reminder_list_page=0,
    )
    reminders = notifications_client.list_reminders(user_id)
    keyboard = get_reminders_list_keyboard(reminders)
    text = (
        "🔔 Уведомления:" if reminders else "🔔 Уведомления:\n\nНапоминаний пока нет\\."
    )
    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")
    logger.info(f"User {user_id} cancelled reminder action")
