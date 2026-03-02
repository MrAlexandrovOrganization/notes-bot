"""Main menu keyboard for the Telegram bot."""

from telegram import InlineKeyboardButton, InlineKeyboardMarkup


def get_main_menu_keyboard(active_date: str) -> InlineKeyboardMarkup:
    """
    Generate the main menu keyboard with action buttons.

    Args:
        active_date: Currently active date in DD-MMM-YYYY format

    Returns:
        InlineKeyboardMarkup with main menu buttons
    """
    keyboard = [
        [
            InlineKeyboardButton("📊 Оценка", callback_data="menu:rating"),
            InlineKeyboardButton("✅ Задачи", callback_data="menu:tasks"),
        ],
        [
            InlineKeyboardButton("📝 Заметка", callback_data="menu:note"),
            InlineKeyboardButton("📅 Календарь", callback_data="menu:calendar"),
        ],
    ]

    return InlineKeyboardMarkup(keyboard)
