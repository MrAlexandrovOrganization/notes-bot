"""Tasks keyboard for the Telegram bot."""

from typing import Any
from telegram import InlineKeyboardButton, InlineKeyboardMarkup


def get_tasks_keyboard(
    tasks: list[dict[str, Any]], current_page: int = 0, tasks_per_page: int = 5
) -> InlineKeyboardMarkup:
    """
    Generate keyboard for displaying and managing tasks.

    Args:
        tasks: List of task dicts with keys 'text', 'completed', 'index'
        current_page: Current page number (0-based)
        tasks_per_page: Number of tasks per page
    """
    keyboard: list[list[InlineKeyboardButton]] = []

    total_pages = (len(tasks) + tasks_per_page - 1) // tasks_per_page
    start_idx = current_page * tasks_per_page
    end_idx = min(start_idx + tasks_per_page, len(tasks))

    for task in tasks[start_idx:end_idx]:
        checkbox = "✅" if task["completed"] else "❌"
        keyboard.append(
            [
                InlineKeyboardButton(
                    f"{checkbox} {task['text']}",
                    callback_data=f"task:toggle:{task['index']}",
                )
            ]
        )

    keyboard.append(
        [InlineKeyboardButton("➕ Добавить задачу", callback_data="task:add")]
    )

    if total_pages > 1:
        nav_buttons: list[InlineKeyboardButton] = []
        if current_page > 0:
            nav_buttons.append(
                InlineKeyboardButton("◀", callback_data=f"task:page:{current_page - 1}")
            )
        nav_buttons.append(
            InlineKeyboardButton(
                f"{current_page + 1}/{total_pages}", callback_data="task:noop"
            )
        )
        if current_page < total_pages - 1:
            nav_buttons.append(
                InlineKeyboardButton("▶", callback_data=f"task:page:{current_page + 1}")
            )
        keyboard.append(nav_buttons)

    keyboard.append([InlineKeyboardButton("◀ Назад", callback_data="task:back")])

    return InlineKeyboardMarkup(keyboard)


def get_task_add_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        [[InlineKeyboardButton("❌ Отмена", callback_data="task:cancel")]]
    )
