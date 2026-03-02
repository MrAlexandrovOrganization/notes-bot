"""State manager for handling user contexts in the Notes Bot."""

import logging
from datetime import datetime
from typing import Any, Dict

from .context import UserContext, UserState
from core.utils import get_today_filename

logger = logging.getLogger(__name__)


class StateManager:
    """
    Manages user contexts and states for the telegram bot.

    Stores user contexts in memory and provides methods to create,
    retrieve, update, and reset user states.
    """

    def __init__(self):
        """Initialize the state manager with an empty context storage."""
        self._contexts: Dict[int, UserContext] = {}

    def get_context(self, user_id: int) -> UserContext:
        """
        Get or create a user context.

        If the user context doesn't exist, creates a new one with:
        - Current date as active_date (from get_today_filename())
        - Current month and year for calendar
        - IDLE state

        Args:
            user_id: Telegram user ID

        Returns:
            UserContext object for the specified user
        """
        if user_id not in self._contexts:
            # Get current date in the required format
            today_filename = get_today_filename()
            # Remove .md extension to get just the date
            active_date = today_filename.replace(".md", "")

            # Get current month and year
            now = datetime.now()

            self._contexts[user_id] = UserContext(
                user_id=user_id,
                state=UserState.IDLE,
                active_date=active_date,
                calendar_month=now.month,
                calendar_year=now.year,
                task_page=0,
                last_message_id=None,
            )

        return self._contexts[user_id]

    def update_context(self, user_id: int, **kwargs: Any) -> None:
        """
        Update specific fields of a user's context.

        Creates a new context if it doesn't exist, then updates
        the specified fields.

        Args:
            user_id: Telegram user ID
            **kwargs: Fields to update (e.g., state=UserState.WAITING_RATING)

        Example:
            manager.update_context(123, state=UserState.TASKS_VIEW, task_page=1)
        """
        context = self.get_context(user_id)

        for key, value in kwargs.items():
            if hasattr(context, key):
                setattr(context, key, value)

    def reset_context(self, user_id: int) -> None:
        """
        Reset user context to IDLE state.

        Resets the state to IDLE and clears task_page and last_message_id,
        but preserves active_date and calendar settings.

        Args:
            user_id: Telegram user ID
        """
        context = self.get_context(user_id)
        context.state = UserState.IDLE
        context.task_page = 0
        context.last_message_id = None

    def set_active_date(self, user_id: int, date: str) -> None:
        """
        Set the active date for a user.

        Args:
            user_id: Telegram user ID
            date: Date string in format DD-MMM-YYYY (e.g., 11-Oct-2025)
        """
        context = self.get_context(user_id)
        context.active_date = date
