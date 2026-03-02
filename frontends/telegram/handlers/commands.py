"""Command handlers for the Notes Bot."""

import logging
from telegram import Update
from telegram.ext import ContextTypes

from ..config import ROOT_ID
from ..keyboards.main_menu import get_main_menu_keyboard
from ..states import state_manager
from ..utils import escape_markdown_v2

logger = logging.getLogger(__name__)


async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    """
    Handle /start command.

    Checks authorization, initializes user context, and displays welcome message
    with main menu.

    Args:
        update: Telegram update object
        context: Bot context
    """
    if not update.message or not update.effective_user:
        return

    user_id = update.effective_user.id

    # Check authorization
    if ROOT_ID and user_id != ROOT_ID:
        await update.message.reply_text("⛔ Unauthorized access.")
        logger.warning(f"Unauthorized access attempt from user {user_id}")
        return

    # Initialize or get user context
    user_context = state_manager.get_context(user_id)

    # Get active date for display
    active_date = user_context.active_date

    # Prepare welcome message
    welcome_text = (
        f"👋 Добро пожаловать\\!\n\n"
        f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\n"
        f"Выберите действие:"
    )

    # Get main menu keyboard
    keyboard = get_main_menu_keyboard(active_date)

    # Send welcome message with menu
    await update.message.reply_text(
        welcome_text, reply_markup=keyboard, parse_mode="MarkdownV2"
    )

    logger.info(f"User {user_id} started the bot")
