"""Main bot module."""

import logging
from telegram import Update
from telegram.ext import (
    Application,
    MessageHandler,
    CommandHandler,
    CallbackQueryHandler,
    ContextTypes,
    filters,
)
from .config import BOT_TOKEN, ROOT_ID

# Handlers
from .handlers import (
    cmd_start,
    handle_text_message,
    handle_callback,
)

logger = logging.getLogger(__name__)


def main() -> None:
    """Start the bot"""
    if not BOT_TOKEN:
        logger.error("BOT_TOKEN not found in environment variables")
        return

    if not ROOT_ID:
        logger.error("ROOT_ID not found in environment variables")
        return

    # Create application
    application = Application.builder().token(BOT_TOKEN).build()

    # Register handlers
    application.add_handler(CommandHandler("start", cmd_start))
    application.add_handler(CallbackQueryHandler(handle_callback))
    application.add_handler(
        MessageHandler(filters.TEXT & ~filters.COMMAND, handle_text_message)
    )

    # Add error handler
    async def error_handler(update: Update, context: ContextTypes.DEFAULT_TYPE):
        """Handle errors"""
        logger.error(f"Update {update} caused error {context.error}")
        if update and update.effective_message:
            try:
                await update.effective_message.reply_text(
                    "❌ Произошла ошибка\\. Попробуйте /start", parse_mode="MarkdownV2"
                )
            except Exception:
                pass

    application.add_error_handler(error_handler)

    logger.info("Bot started successfully")

    # Run the bot
    application.run_polling(allowed_updates=Update.ALL_TYPES)


if __name__ == "__main__":
    main()
