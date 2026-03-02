"""User context and state definitions for the Notes Bot."""

from dataclasses import dataclass
from enum import Enum
from typing import Optional


class UserState(Enum):
    """Enumeration of possible user states in the bot."""

    IDLE = "idle"
    WAITING_RATING = "waiting_rating"
    TASKS_VIEW = "tasks_view"
    WAITING_NEW_TASK = "waiting_new_task"
    CALENDAR_VIEW = "calendar_view"


@dataclass
class UserContext:
    """
    Context information for a user's current session.

    Attributes:
        user_id: Telegram user ID
        state: Current state of the user in the bot workflow
        active_date: Currently selected date in format DD-MMM-YYYY (e.g., 11-Oct-2025)
        calendar_month: Month being viewed in calendar (1-12)
        calendar_year: Year being viewed in calendar
        task_page: Current page number for task pagination (0-indexed)
        last_message_id: ID of the last bot message for editing purposes
    """

    user_id: int
    state: UserState = UserState.IDLE
    active_date: str = ""
    calendar_month: int = 0
    calendar_year: int = 0
    task_page: int = 0
    last_message_id: Optional[int] = None
