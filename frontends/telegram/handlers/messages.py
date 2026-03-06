"""Text message handlers for the Notes Bot."""

import logging
from telegram import Update
from telegram.ext import ContextTypes

from ..config import ROOT_ID
from ..grpc_client import core_client
from ..states.context import UserState
from ..states import state_manager
from ..keyboards.main_menu import get_main_menu_keyboard
from ..utils import escape_markdown_v2
from .reminders import (
    handle_reminder_title_input,
    handle_reminder_param_input,
)

logger = logging.getLogger(__name__)


async def handle_text_message(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    if not update.message or not update.effective_user or not update.message.text:
        return

    user_id = update.effective_user.id
    text = update.message.text.strip()

    if ROOT_ID and user_id != ROOT_ID:
        await update.message.reply_text("⛔ Unauthorized access.")
        logger.warning(f"Unauthorized message from user {user_id}")
        return

    user_context = state_manager.get_context(user_id)
    current_state = user_context.state
    active_date = user_context.active_date

    try:
        if current_state == UserState.WAITING_RATING:
            try:
                rating = int(text)
                if rating < 0 or rating > 10:
                    await update.message.reply_text(
                        "❌ Оценка должна быть от 0 до 10\\. Попробуйте снова\\.",
                        parse_mode="MarkdownV2",
                    )
                    return

                if core_client.update_rating(active_date, rating):
                    state_manager.update_context(user_id, state=UserState.IDLE)
                    await update.message.reply_text(
                        f"✅ Оценка {rating} сохранена\\!",
                        reply_markup=get_main_menu_keyboard(active_date),
                        parse_mode="MarkdownV2",
                    )
                    logger.info(f"User {user_id} set rating {rating} for {active_date}")
                else:
                    await update.message.reply_text(
                        "❌ Ошибка при сохранении оценки\\.", parse_mode="MarkdownV2"
                    )

            except ValueError:
                await update.message.reply_text(
                    "❌ Пожалуйста, введите число от 0 до 10\\.",
                    parse_mode="MarkdownV2",
                )

        elif current_state == UserState.REMINDER_CREATE_TITLE:
            await handle_reminder_title_input(update, user_id, text)

        elif current_state in (
            UserState.REMINDER_CREATE_TIME,
            UserState.REMINDER_CREATE_DAY,
            UserState.REMINDER_CREATE_INTERVAL,
            UserState.REMINDER_CREATE_DATE,
        ):
            await handle_reminder_param_input(update, user_id, text)

        elif current_state == UserState.WAITING_NEW_TASK:
            if core_client.add_task(active_date, text):
                state_manager.update_context(user_id, state=UserState.TASKS_VIEW)
                await update.message.reply_text(
                    f"✅ Задача добавлена: {escape_markdown_v2(text)}",
                    parse_mode="MarkdownV2",
                )
                logger.info(f"User {user_id} added task: {text}")
                await update.message.reply_text(
                    'Используйте кнопку "Задачи" для просмотра\\.',
                    reply_markup=get_main_menu_keyboard(active_date),
                    parse_mode="MarkdownV2",
                )
            else:
                await update.message.reply_text(
                    "❌ Ошибка при добавлении задачи\\.", parse_mode="MarkdownV2"
                )

        else:
            if core_client.append_to_note(active_date, text):
                await update.message.reply_text(
                    f"✅ Текст добавлен в заметку {escape_markdown_v2(active_date)}",
                    reply_markup=get_main_menu_keyboard(active_date),
                    parse_mode="MarkdownV2",
                )
                logger.info(f"User {user_id} added text to {active_date}")
            else:
                await update.message.reply_text(
                    "❌ Ошибка при сохранении текста\\.", parse_mode="MarkdownV2"
                )

    except Exception as e:
        logger.error(f"Error handling text message from user {user_id}: {e}")
        await update.message.reply_text(
            "❌ Произошла ошибка при обработке сообщения\\.", parse_mode="MarkdownV2"
        )
