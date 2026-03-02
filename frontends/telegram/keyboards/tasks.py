"""Tasks keyboard for the Telegram bot."""

from typing import List, Dict, Any
from telegram import InlineKeyboardButton, InlineKeyboardMarkup


def get_tasks_keyboard(
    tasks: List[Dict[str, Any]], current_page: int = 0, tasks_per_page: int = 5
) -> InlineKeyboardMarkup:
    """
    Generate keyboard for displaying and managing tasks.

    Args:
        tasks: List of task dictionaries with keys 'text', 'completed', 'index'
        current_page: Current page number (0-based)
        tasks_per_page: Number of tasks to display per page

    Returns:
        InlineKeyboardMarkup with task buttons and navigation
    """
    keyboard: List[List[InlineKeyboardButton]] = []

    # Calculate pagination
    total_pages = (len(tasks) + tasks_per_page - 1) // tasks_per_page
    start_idx = current_page * tasks_per_page
    end_idx = min(start_idx + tasks_per_page, len(tasks))

    # Add task buttons
    for task in tasks[start_idx:end_idx]:
        checkbox = "✅" if task["completed"] else "❌"
        button_text = f"{checkbox} {task['text']}"
        callback_data = f"task:toggle:{task['index']}"
        keyboard.append(
            [InlineKeyboardButton(button_text, callback_data=callback_data)]
        )

    # Add "Add Task" button
    keyboard.append(
        [InlineKeyboardButton("➕ Добавить задачу", callback_data="task:add")]
    )

    # Add pagination if needed
    if total_pages > 1:
        nav_buttons: List[InlineKeyboardButton] = []
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

    # Add back button
    keyboard.append([InlineKeyboardButton("◀ Назад", callback_data="task:back")])

    return InlineKeyboardMarkup(keyboard)


def get_task_add_keyboard() -> InlineKeyboardMarkup:
    """
    Generate keyboard for adding a new task.

    Returns:
        InlineKeyboardMarkup with cancel button
    """
    keyboard = [[InlineKeyboardButton("❌ Отмена", callback_data="task:cancel")]]

    return InlineKeyboardMarkup(keyboard)
