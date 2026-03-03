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
from ..utils import escape_markdown_v2
from .reminders import (
    handle_menu_notifications,
    handle_reminder_create,
    handle_reminder_type_select,
    handle_reminder_delete,
    handle_reminder_done,
    handle_reminder_postpone_days,
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


async def handle_callback(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    """
    Main callback query router.

    Parses callback_data and routes to appropriate handler based on action prefix.
    Format: "action:param1:param2:..."

    Args:
        update: Telegram update object
        context: Bot context
    """
    query = update.callback_query
    if not query or not query.data or not update.effective_user:
        return

    await query.answer()

    user_id = update.effective_user.id

    # Check authorization
    if ROOT_ID and user_id != ROOT_ID:
        await query.edit_message_text("⛔ Unauthorized access.")
        logger.warning(f"Unauthorized callback from user {user_id}")
        return

    callback_data = query.data
    parts = callback_data.split(":")

    if len(parts) == 0:
        logger.error(f"Invalid callback_data: {callback_data}")
        return

    action = parts[0]

    try:
        # Route to appropriate handler
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
                pass  # No operation for pagination display

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
                pass  # No operation for header/weekday display

        elif action == "reminder":
            if len(parts) < 2:
                logger.error(f"Invalid reminder callback: {callback_data}")
                return

            reminder_action = parts[1]

            if reminder_action == "create":
                await handle_reminder_create(query, user_id)
            elif reminder_action == "type" and len(parts) >= 3:
                await handle_reminder_type_select(query, user_id, parts[2])
            elif reminder_action == "delete" and len(parts) >= 3:
                await handle_reminder_delete(query, user_id, int(parts[2]))
            elif reminder_action == "done" and len(parts) >= 3:
                await handle_reminder_done(query, user_id, int(parts[2]))
            elif reminder_action == "postpone" and len(parts) >= 4:
                await handle_reminder_postpone_days(
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
        await query.edit_message_text(
            "⏳ Сервис уведомлений ещё запускается\\. Попробуйте через несколько секунд\\.",
            parse_mode="MarkdownV2",
        )
    except Exception as e:
        logger.error(f"Error handling callback {callback_data}: {e}")
        await query.edit_message_text(
            "❌ Произошла ошибка при обработке действия\\.", parse_mode="MarkdownV2"
        )


# Menu handlers


async def handle_menu_rating(query: CallbackQuery, user_id: int) -> None:
    """Handle rating menu button - request rating input."""
    state_manager.get_context(user_id)

    # Update state to waiting for rating
    state_manager.update_context(user_id, state=UserState.WAITING_RATING)

    # Send rating request
    text = "📊 Введите оценку дня \\(0\\-10\\):"

    await query.edit_message_text(text, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} requested rating input")


async def handle_menu_tasks(query: CallbackQuery, user_id: int) -> None:
    """Handle tasks menu button - show tasks list."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Update state
    state_manager.update_context(user_id, state=UserState.TASKS_VIEW, task_page=0)

    # Ensure note exists
    core_client.ensure_note(active_date)

    # Get tasks
    tasks = core_client.get_tasks(active_date)

    # Generate tasks keyboard
    keyboard = get_tasks_keyboard(tasks, current_page=0)

    # Prepare message
    if tasks:
        text = f"✅ Задачи на {escape_markdown_v2(active_date)}:\n\nВсего задач: {len(tasks)}"
    else:
        text = f"✅ Задачи на {escape_markdown_v2(active_date)}:\n\nЗадач пока нет\\."

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} opened tasks view")


async def handle_menu_note(query: CallbackQuery, user_id: int) -> None:
    """Handle note menu button - display current note."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Ensure note exists
    core_client.ensure_note(active_date)

    # Read note
    content = core_client.get_note(active_date)
    if not content:
        await query.edit_message_text(
            "❌ Не удалось прочитать заметку\\.", parse_mode="MarkdownV2"
        )
        return

    # Get rating if exists
    rating = core_client.get_rating(active_date)
    rating_text = (
        f"Оценка: {rating}" if rating is not None else "Оценка: не установлена"
    )

    # Prepare note preview
    preview = content[:NOTE_PREVIEW_MAX_CHARS]
    if len(content) > NOTE_PREVIEW_MAX_CHARS:
        preview += "..."

    # Escape for MarkdownV2
    preview_escaped = escape_markdown_v2(preview)
    rating_escaped = escape_markdown_v2(rating_text)

    text = (
        f"📝 Заметка {escape_markdown_v2(active_date)}\n\n"
        f"{rating_escaped}\n\n"
        f"```\n{preview_escaped}\n```"
    )

    # Get main menu keyboard
    keyboard = get_main_menu_keyboard(active_date)

    try:
        # TODO: Check if text and keyboard are the same
        await query.edit_message_text(
            text, reply_markup=keyboard, parse_mode="MarkdownV2"
        )
    except Exception as e:
        logger.warning(f"Error editing message, probably note did not changed: {e}")

    logger.info(f"User {user_id} viewed note for {active_date}")


async def handle_menu_calendar(query: CallbackQuery, user_id: int) -> None:
    """Handle calendar menu button - show calendar."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Update state
    state_manager.update_context(user_id, state=UserState.CALENDAR_VIEW)

    # Get existing dates
    existing_dates = core_client.get_existing_dates()

    # Generate calendar keyboard
    keyboard = get_calendar_keyboard(
        user_context.calendar_year,
        user_context.calendar_month,
        active_date,
        existing_dates,
    )

    text = f"📅 Календарь\n\nАктивная дата: {escape_markdown_v2(active_date)}"

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} opened calendar")


# Task handlers


async def handle_task_toggle(
    query: CallbackQuery, user_id: int, task_index: int
) -> None:
    """Handle task toggle button - switch task completion status."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Toggle task
    if core_client.toggle_task(active_date, task_index):
        # Re-fetch and display updated tasks
        tasks = core_client.get_tasks(active_date)
        keyboard = get_tasks_keyboard(tasks, current_page=user_context.task_page)

        text = f"✅ Задачи на {escape_markdown_v2(active_date)}:\n\nВсего задач: {len(tasks)}"

        await query.edit_message_text(
            text, reply_markup=keyboard, parse_mode="MarkdownV2"
        )

        logger.info(f"User {user_id} toggled task {task_index}")
    else:
        await query.answer("❌ Ошибка при переключении задачи", show_alert=True)


async def handle_task_add(query: CallbackQuery, user_id: int) -> None:
    """Handle add task button - request new task text."""
    # Update state
    state_manager.update_context(user_id, state=UserState.WAITING_NEW_TASK)

    # Request task text
    text = "➕ Введите текст новой задачи:"
    keyboard = get_task_add_keyboard()

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} started adding new task")


async def handle_task_page(query: CallbackQuery, user_id: int, page: int) -> None:
    """Handle task pagination - change page."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Update page
    state_manager.update_context(user_id, task_page=page)

    # Fetch tasks
    tasks = core_client.get_tasks(active_date)
    keyboard = get_tasks_keyboard(tasks, current_page=page)

    text = (
        f"✅ Задачи на {escape_markdown_v2(active_date)}:\n\nВсего задач: {len(tasks)}"
    )

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} changed to task page {page}")


async def handle_task_back(query: CallbackQuery, user_id: int) -> None:
    """Handle task back button - return to main menu."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Reset state
    state_manager.update_context(user_id, state=UserState.IDLE)

    # Show main menu
    text = f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\nВыберите действие:"
    keyboard = get_main_menu_keyboard(active_date)

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} returned to main menu from tasks")


async def handle_task_cancel(query: CallbackQuery, user_id: int) -> None:
    """Handle task cancel button - cancel adding new task."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Return to tasks view
    state_manager.update_context(user_id, state=UserState.TASKS_VIEW)

    # Fetch tasks
    tasks = core_client.get_tasks(active_date)
    keyboard = get_tasks_keyboard(tasks, current_page=user_context.task_page)

    text = (
        f"✅ Задачи на {escape_markdown_v2(active_date)}:\n\nВсего задач: {len(tasks)}"
    )

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} cancelled adding task")


# Calendar handlers


async def handle_cal_prev(query: CallbackQuery, user_id: int) -> None:
    """Handle calendar previous month button."""
    user_context = state_manager.get_context(user_id)

    # Calculate previous month
    month = user_context.calendar_month
    year = user_context.calendar_year

    if month == 1:
        month = 12
        year -= 1
    else:
        month -= 1

    # Update context
    state_manager.update_context(user_id, calendar_month=month, calendar_year=year)

    # Get existing dates
    existing_dates = core_client.get_existing_dates()

    # Generate calendar
    keyboard = get_calendar_keyboard(
        year, month, user_context.active_date, existing_dates
    )

    text = (
        f"📅 Календарь\n\nАктивная дата: {escape_markdown_v2(user_context.active_date)}"
    )

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} navigated to {month}/{year}")


async def handle_cal_next(query: CallbackQuery, user_id: int) -> None:
    """Handle calendar next month button."""
    user_context = state_manager.get_context(user_id)

    # Calculate next month
    month = user_context.calendar_month
    year = user_context.calendar_year

    if month == 12:
        month = 1
        year += 1
    else:
        month += 1

    # Update context
    state_manager.update_context(user_id, calendar_month=month, calendar_year=year)

    # Get existing dates
    existing_dates = core_client.get_existing_dates()

    # Generate calendar
    keyboard = get_calendar_keyboard(
        year, month, user_context.active_date, existing_dates
    )

    text = (
        f"📅 Календарь\n\nАктивная дата: {escape_markdown_v2(user_context.active_date)}"
    )

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} navigated to {month}/{year}")


async def handle_cal_select(query: CallbackQuery, user_id: int, date: str) -> None:
    """Handle calendar date selection."""
    # Set as active date
    state_manager.set_active_date(user_id, date)

    # Create note if doesn't exist
    core_client.ensure_note(date)

    # Reset to IDLE state
    state_manager.update_context(user_id, state=UserState.IDLE)

    # Show main menu with new active date
    text = (
        f"✅ Выбрана дата: {escape_markdown_v2(date)}\n\n"
        f"📅 Активная дата: {escape_markdown_v2(date)}\n\n"
        f"Выберите действие:"
    )
    keyboard = get_main_menu_keyboard(date)

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} selected date {date}")


async def handle_cal_today(query: CallbackQuery, user_id: int) -> None:
    """Handle calendar today button - return to current date."""
    # Get today's date
    today_date = core_client.get_today_date()

    # Set as active date
    state_manager.set_active_date(user_id, today_date)

    # Update calendar to current month
    now = datetime.now()
    state_manager.update_context(
        user_id, calendar_month=now.month, calendar_year=now.year
    )

    # Get existing dates
    existing_dates = core_client.get_existing_dates()

    # Generate calendar
    keyboard = get_calendar_keyboard(now.year, now.month, today_date, existing_dates)

    text = f"📅 Календарь\n\nАктивная дата: {escape_markdown_v2(today_date)}"

    try:
        await query.edit_message_text(
            text, reply_markup=keyboard, parse_mode="MarkdownV2"
        )
    except Exception as e:
        logger.warning(f"Error editing date, probably date did not changed: {e}")

    logger.info(f"User {user_id} returned to today: {today_date}")


async def handle_cal_back(query: CallbackQuery, user_id: int) -> None:
    """Handle calendar back button - return to main menu."""
    user_context = state_manager.get_context(user_id)
    active_date = user_context.active_date

    # Reset state
    state_manager.update_context(user_id, state=UserState.IDLE)

    # Show main menu
    text = f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\nВыберите действие:"
    keyboard = get_main_menu_keyboard(active_date)

    await query.edit_message_text(text, reply_markup=keyboard, parse_mode="MarkdownV2")

    logger.info(f"User {user_id} returned to main menu from calendar")
