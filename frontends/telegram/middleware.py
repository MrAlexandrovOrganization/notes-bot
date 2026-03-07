import logging
from typing import Optional

from telegram import CallbackQuery, InlineKeyboardMarkup, Update

logger = logging.getLogger(__name__)


async def reply_message(
    update_or_query: Update | CallbackQuery,
    text: str,
    keyboard: Optional[InlineKeyboardMarkup] = None,
):
    if isinstance(update_or_query, Update):
        if update_or_query.message is None:
            logger.warning("Update message is None")
            return
        await update_or_query.message.reply_text(
            text,
            reply_markup=keyboard,
            parse_mode="MarkdownV2",
            disable_notification=True,
        )
    else:
        await update_or_query.edit_message_text(
            text, reply_markup=keyboard, parse_mode="MarkdownV2"
        )
