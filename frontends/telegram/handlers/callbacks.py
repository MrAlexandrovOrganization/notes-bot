"""Callback query handlers for the Notes Bot."""

import logging
from datetime import datetime
from telegram import CallbackQuery, Update
from telegram.ext import ContextTypes

from ..config import ROOT_ID
from ..grpc_client import core_client
from ..notifications_client import NotificationsUnavailableError
from ..states import UserState, state_manager
from ..keyboards.main_menu import get_main_menu_keyboard
from ..keyboards.tasks import get_tasks_keyboard, get_task_add_keyboard
from ..keyboards.calendar import get_calendar_keyboard
from ..middleware import reply_message
from ..utils import escape_markdown_v2
from .reminders import (
    handle_menu_notifications,
    handle_reminder_page,
    handle_reminder_create,
    handle_reminder_type_select,
    handle_reminder_task_confirm,
    handle_reminder_delete,
    handle_reminder_done,
    handle_reminder_postpone_days,
    handle_reminder_postpone_hours,
    handle_reminder_custom_date,
    handle_reminder_back,
    handle_reminder_cancel,
    handle_reminder_cal_prev,
    handle_reminder_cal_next,
    handle_reminder_cal_today,
    handle_reminder_cal_select,
)

logger = logging.getLogger(__name__)

NOTE_PREVIEW_MAX_CHARS = 3800


def _step_month(month: int, year: int, delta: int) -> tuple[int, int]:
    month += delta
    if month < 1:
        return 12, year - 1
    if month > 12:
        return 1, year + 1
    return month, year


async def _show_tasks(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    tasks = core_client.get_tasks(ctx.active_date)
    keyboard = get_tasks_keyboard(tasks, current_page=ctx.task_page)
    text = (
        f"✅ Задачи на {escape_markdown_v2(ctx.active_date)}:\n\nВсего задач: {len(tasks)}"
        if tasks
        else f"✅ Задачи на {escape_markdown_v2(ctx.active_date)}:\n\nЗадач пока нет\\."
    )
    await reply_message(
        update_or_query=query,
        text=text,
        keyboard=keyboard,
    )


async def _show_calendar(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    existing_dates = core_client.get_existing_dates()
    keyboard = get_calendar_keyboard(
        ctx.calendar_year, ctx.calendar_month, ctx.active_date, existing_dates
    )
    text = f"📅 Календарь\n\nАктивная дата: {escape_markdown_v2(ctx.active_date)}"
    try:
        await reply_message(
            update_or_query=query,
            text=text,
            keyboard=keyboard,
        )
    except Exception as e:
        if "Message is not modified" not in str(e):
            raise


async def _show_main_menu(query: CallbackQuery, user_id: int) -> None:
    active_date = state_manager.get_context(user_id).active_date
    text = f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\nВыберите действие:"
    await reply_message(
        update_or_query=query,
        text=text,
        keyboard=get_main_menu_keyboard(active_date),
    )


async def handle_callback(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    """
    Main callback query router.

    Parses callback_data and routes to appropriate handler based on action prefix.
    Format: "action:param1:param2:..."
    """
    query = update.callback_query
    if not query or not query.data or not update.effective_user:
        return

    await query.answer()

    user_id = update.effective_user.id

    if ROOT_ID and user_id != ROOT_ID:
        await reply_message(
            update_or_query=query,
            text="⛔ Unauthorized access.",
        )
        logger.warning(f"Unauthorized callback from user {user_id}")
        return

    callback_data = query.data
    parts = callback_data.split(":")

    if len(parts) == 0:
        logger.error(f"Invalid callback_data: {callback_data}")
        return

    action = parts[0]

    try:
        if action == "menu":
            if len(parts) < 2:
                logger.error(f"Invalid menu callback: {callback_data}")
                return

            menu_action = parts[1]

            if menu_action == "rating":
                await handle_menu_rating(query, user_id)
            elif menu_action == "tasks":
                await handle_menu_tasks(query, user_id)
            elif menu_action == "note":
                await handle_menu_note(query, user_id)
            elif menu_action == "calendar":
                await handle_menu_calendar(query, user_id)
            elif menu_action == "notifications":
                await handle_menu_notifications(query, user_id)

        elif action == "task":
            if len(parts) < 2:
                logger.error(f"Invalid task callback: {callback_data}")
                return

            task_action = parts[1]

            if task_action == "toggle" and len(parts) >= 3:
                task_index = int(parts[2])
                await handle_task_toggle(query, user_id, task_index)
            elif task_action == "add":
                await handle_task_add(query, user_id)
            elif task_action == "page" and len(parts) >= 3:
                page = int(parts[2])
                await handle_task_page(query, user_id, page)
            elif task_action == "back":
                await handle_task_back(query, user_id)
            elif task_action == "cancel":
                await handle_task_cancel(query, user_id)
            elif task_action == "noop":
                pass

        elif action == "cal":
            if len(parts) < 2:
                logger.error(f"Invalid calendar callback: {callback_data}")
                return

            cal_action = parts[1]

            if cal_action == "prev":
                await handle_cal_prev(query, user_id)
            elif cal_action == "next":
                await handle_cal_next(query, user_id)
            elif cal_action == "select" and len(parts) >= 3:
                date = parts[2]
                await handle_cal_select(query, user_id, date)
            elif cal_action == "today":
                await handle_cal_today(query, user_id)
            elif cal_action == "back":
                await handle_cal_back(query, user_id)
            elif cal_action == "noop":
                pass

        elif action == "reminder":
            if len(parts) < 2:
                logger.error(f"Invalid reminder callback: {callback_data}")
                return

            reminder_action = parts[1]

            if reminder_action == "create":
                await handle_reminder_create(query, user_id)
            elif reminder_action == "page" and len(parts) >= 3:
                await handle_reminder_page(query, user_id, int(parts[2]))
            elif reminder_action == "type" and len(parts) >= 3:
                await handle_reminder_type_select(query, user_id, parts[2])
            elif reminder_action == "task_confirm" and len(parts) >= 3:
                await handle_reminder_task_confirm(query, user_id, parts[2] == "yes")
            elif reminder_action == "delete" and len(parts) >= 3:
                await handle_reminder_delete(query, user_id, int(parts[2]))
            elif reminder_action == "done" and len(parts) >= 3:
                reminder_id = int(parts[2])
                create_task_flag = int(parts[3]) if len(parts) > 3 else 0
                date_str = parts[4] if len(parts) > 4 else ""
                await handle_reminder_done(
                    query, user_id, reminder_id, create_task_flag, date_str
                )
            elif reminder_action == "postpone" and len(parts) >= 4:
                await handle_reminder_postpone_days(
                    query, user_id, int(parts[2]), int(parts[3])
                )
            elif reminder_action == "postpone_hours" and len(parts) >= 4:
                await handle_reminder_postpone_hours(
                    query, user_id, int(parts[2]), int(parts[3])
                )
            elif reminder_action == "custom_date" and len(parts) >= 3:
                await handle_reminder_custom_date(query, user_id, int(parts[2]))
            elif reminder_action == "cal" and len(parts) >= 4:
                cal_sub = parts[2]
                context_name = parts[3]
                if cal_sub == "prev":
                    await handle_reminder_cal_prev(query, user_id, context_name)
                elif cal_sub == "next":
                    await handle_reminder_cal_next(query, user_id, context_name)
                elif cal_sub == "today":
                    await handle_reminder_cal_today(query, user_id, context_name)
                elif cal_sub == "select" and len(parts) >= 5:
                    date_str = parts[3]
                    context_name = parts[4]
                    await handle_reminder_cal_select(
                        query, user_id, date_str, context_name
                    )
            elif reminder_action == "back":
                await handle_reminder_back(query, user_id)
            elif reminder_action == "cancel":
                await handle_reminder_cancel(query, user_id)
            elif reminder_action == "noop":
                pass

        else:
            logger.warning(f"Unknown callback action: {action}")

    except NotificationsUnavailableError:
        await reply_message(
            update_or_query=query,
            text="⏳ Сервис уведомлений ещё запускается\\. Попробуйте через несколько секунд\\.",
        )
    except Exception as e:
        logger.error(f"Error handling callback {callback_data}: {e}")
        await reply_message(
            update_or_query=query,
            text="❌ Произошла ошибка при обработке действия\\.",
        )


# ── Menu handlers ──────────────────────────────────────────────────────────────


async def handle_menu_rating(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.WAITING_RATING)
    await reply_message(
        update_or_query=query,
        text="📊 Введите оценку дня \\(0\\-10\\):",
    )
    logger.info(f"User {user_id} requested rating input")


async def handle_menu_tasks(query: CallbackQuery, user_id: int) -> None:
    active_date = state_manager.get_context(user_id).active_date
    state_manager.update_context(user_id, state=UserState.TASKS_VIEW, task_page=0)
    core_client.ensure_note(active_date)
    await _show_tasks(query, user_id)
    logger.info(f"User {user_id} opened tasks view")


async def handle_menu_note(query: CallbackQuery, user_id: int) -> None:
    active_date = state_manager.get_context(user_id).active_date
    core_client.ensure_note(active_date)
    content = core_client.get_note(active_date)
    if not content:
        await reply_message(
            update_or_query=query,
            text="❌ Не удалось прочитать заметку\\.",
        )
        return

    rating = core_client.get_rating(active_date)
    rating_text = (
        f"Оценка: {rating}" if rating is not None else "Оценка: не установлена"
    )
    preview = content[:NOTE_PREVIEW_MAX_CHARS]
    if len(content) > NOTE_PREVIEW_MAX_CHARS:
        preview += "..."

    text = (
        f"📝 Заметка {escape_markdown_v2(active_date)}\n\n"
        f"{escape_markdown_v2(rating_text)}\n\n"
        f"```\n{escape_markdown_v2(preview)}\n```"
    )
    try:
        await reply_message(
            update_or_query=query,
            text=text,
            keyboard=get_main_menu_keyboard(active_date),
        )
    except Exception as e:
        logger.info(f"Error editing message, probably note did not changed: {e}")
    logger.info(f"User {user_id} viewed note for {active_date}")


async def handle_menu_calendar(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.CALENDAR_VIEW)
    await _show_calendar(query, user_id)
    logger.info(f"User {user_id} opened calendar")


# ── Task handlers ──────────────────────────────────────────────────────────────


async def handle_task_toggle(
    query: CallbackQuery, user_id: int, task_index: int
) -> None:
    active_date = state_manager.get_context(user_id).active_date
    if core_client.toggle_task(active_date, task_index):
        await _show_tasks(query, user_id)
        logger.info(f"User {user_id} toggled task {task_index}")
    else:
        await query.answer("❌ Ошибка при переключении задачи", show_alert=True)


async def handle_task_add(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.WAITING_NEW_TASK)
    await reply_message(
        update_or_query=query,
        text="➕ Введите текст новой задачи:",
        keyboard=get_task_add_keyboard(),
    )
    logger.info(f"User {user_id} started adding new task")


async def handle_task_page(query: CallbackQuery, user_id: int, page: int) -> None:
    state_manager.update_context(user_id, task_page=page)
    await _show_tasks(query, user_id)
    logger.info(f"User {user_id} changed to task page {page}")


async def handle_task_back(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.IDLE)
    await _show_main_menu(query, user_id)
    logger.info(f"User {user_id} returned to main menu from tasks")


async def handle_task_cancel(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.TASKS_VIEW)
    await _show_tasks(query, user_id)
    logger.info(f"User {user_id} cancelled adding task")


# ── Calendar handlers ──────────────────────────────────────────────────────────


async def handle_cal_prev(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    month, year = _step_month(ctx.calendar_month, ctx.calendar_year, -1)
    state_manager.update_context(user_id, calendar_month=month, calendar_year=year)
    await _show_calendar(query, user_id)
    logger.info(f"User {user_id} navigated to {month}/{year}")


async def handle_cal_next(query: CallbackQuery, user_id: int) -> None:
    ctx = state_manager.get_context(user_id)
    month, year = _step_month(ctx.calendar_month, ctx.calendar_year, +1)
    state_manager.update_context(user_id, calendar_month=month, calendar_year=year)
    await _show_calendar(query, user_id)
    logger.info(f"User {user_id} navigated to {month}/{year}")


async def handle_cal_select(query: CallbackQuery, user_id: int, date: str) -> None:
    state_manager.set_active_date(user_id, date)
    core_client.ensure_note(date)
    state_manager.update_context(user_id, state=UserState.IDLE)
    text = (
        f"✅ Выбрана дата: {escape_markdown_v2(date)}\n\n"
        f"📅 Активная дата: {escape_markdown_v2(date)}\n\n"
        f"Выберите действие:"
    )
    await reply_message(
        update_or_query=query,
        text=text,
        keyboard=get_main_menu_keyboard(date),
    )
    logger.info(f"User {user_id} selected date {date}")


async def handle_cal_today(query: CallbackQuery, user_id: int) -> None:
    today_date = core_client.get_today_date()
    state_manager.set_active_date(user_id, today_date)
    now = datetime.now()
    state_manager.update_context(
        user_id, calendar_month=now.month, calendar_year=now.year
    )
    try:
        await _show_calendar(query, user_id)
    except Exception as e:
        logger.warning(f"Error editing date, probably date did not changed: {e}")
    logger.info(f"User {user_id} returned to today: {today_date}")


async def handle_cal_back(query: CallbackQuery, user_id: int) -> None:
    state_manager.update_context(user_id, state=UserState.IDLE)
    await _show_main_menu(query, user_id)
    logger.info(f"User {user_id} returned to main menu from calendar")
