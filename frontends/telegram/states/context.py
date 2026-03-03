"""User context and state definitions for the Notes Bot."""

from dataclasses import dataclass, field
from enum import Enum
from typing import Optional


class UserState(Enum):
    """Enumeration of possible user states in the bot."""

    IDLE = "idle"
    WAITING_RATING = "waiting_rating"
    TASKS_VIEW = "tasks_view"
    WAITING_NEW_TASK = "waiting_new_task"
    CALENDAR_VIEW = "calendar_view"
    REMINDER_LIST = "reminder_list"
    REMINDER_CREATE_TITLE = "reminder_create_title"
    REMINDER_CREATE_SCHEDULE_TYPE = "reminder_create_schedule_type"
    REMINDER_CREATE_TIME = "reminder_create_time"
    REMINDER_CREATE_DAY = "reminder_create_day"
    REMINDER_CREATE_INTERVAL = "reminder_create_interval"
    REMINDER_CREATE_DATE = "reminder_create_date"
    REMINDER_POSTPONE_DATE = "reminder_postpone_date"


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
        reminder_draft: Accumulates multi-step reminder creation params
        pending_postpone_reminder_id: Reminder ID awaiting custom postpone date
    """

    user_id: int
    state: UserState = UserState.IDLE
    active_date: str = ""
    calendar_month: int = 0
    calendar_year: int = 0
    task_page: int = 0
    last_message_id: Optional[int] = None
    reminder_draft: dict = field(default_factory=dict)
    pending_postpone_reminder_id: Optional[int] = None
    reminder_cal_month: int = 0
    reminder_cal_year: int = 0
