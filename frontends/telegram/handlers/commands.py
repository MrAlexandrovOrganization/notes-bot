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
    if not update.message or not update.effective_user:
        return

    user_id = update.effective_user.id

    if ROOT_ID and user_id != ROOT_ID:
        await update.message.reply_text("⛔ Unauthorized access.")
        logger.warning(f"Unauthorized access attempt from user {user_id}")
        return

    active_date = state_manager.get_context(user_id).active_date

    await update.message.reply_text(
        f"👋 Добро пожаловать\\!\n\n"
        f"📅 Активная дата: {escape_markdown_v2(active_date)}\n\n"
        f"Выберите действие:",
        reply_markup=get_main_menu_keyboard(active_date),
        parse_mode="MarkdownV2",
    )
    logger.info(f"User {user_id} started the bot")
