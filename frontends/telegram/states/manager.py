"""State manager for handling user contexts in the Notes Bot."""

import json
import logging
import os
from datetime import datetime, timedelta, timezone
from typing import Any, Optional

import redis

from ..config import DAY_START_HOUR, TIMEZONE_OFFSET_HOURS
from .context import UserContext, UserState

_TTL_SECONDS = 7 * 24 * 3600  # 7 days

logger = logging.getLogger(__name__)


def _get_today_date() -> str:
    """Return today's date string in DD-MMM-YYYY format."""
    now_utc = datetime.now(timezone.utc)
    local_time = now_utc + timedelta(hours=TIMEZONE_OFFSET_HOURS)
    if local_time.hour < DAY_START_HOUR:
        local_time -= timedelta(days=1)
    return local_time.strftime("%d-%b-%Y")


def _make_redis_client() -> redis.Redis:
    host = os.getenv("REDIS_HOST", "localhost")
    port = int(os.getenv("REDIS_PORT", "6379"))
    return redis.Redis(host=host, port=port, decode_responses=True)


class StateManager:
    """
    Manages user contexts and states for the telegram bot.

    Stores user contexts in Redis (persistent across bot restarts) and
    provides methods to create, retrieve, update, and reset user states.
    """

    def __init__(self, redis_client: Optional[redis.Redis] = None):
        self._redis = redis_client if redis_client is not None else _make_redis_client()

    def _key(self, user_id: int) -> str:
        return f"user_state:{user_id}"

    def _serialize(self, ctx: UserContext) -> str:
        return json.dumps(
            {
                "user_id": ctx.user_id,
                "state": ctx.state.value,
                "active_date": ctx.active_date,
                "calendar_month": ctx.calendar_month,
                "calendar_year": ctx.calendar_year,
                "task_page": ctx.task_page,
                "last_message_id": ctx.last_message_id,
                "reminder_draft": ctx.reminder_draft,
                "pending_postpone_reminder_id": ctx.pending_postpone_reminder_id,
                "reminder_cal_month": ctx.reminder_cal_month,
                "reminder_cal_year": ctx.reminder_cal_year,
                "reminder_list_page": ctx.reminder_list_page,
            }
        )

    def _deserialize(self, data: str) -> UserContext:
        d = json.loads(data)
        return UserContext(
            user_id=d["user_id"],
            state=UserState(d["state"]),
            active_date=d.get("active_date", ""),
            calendar_month=d.get("calendar_month", 0),
            calendar_year=d.get("calendar_year", 0),
            task_page=d.get("task_page", 0),
            last_message_id=d.get("last_message_id"),
            reminder_draft=d.get("reminder_draft", {}),
            pending_postpone_reminder_id=d.get("pending_postpone_reminder_id"),
            reminder_cal_month=d.get("reminder_cal_month", 0),
            reminder_cal_year=d.get("reminder_cal_year", 0),
            reminder_list_page=d.get("reminder_list_page", 0),
        )

    def _save(self, ctx: UserContext) -> None:
        self._redis.setex(self._key(ctx.user_id), _TTL_SECONDS, self._serialize(ctx))

    def get_context(self, user_id: int) -> UserContext:
        """
        Get or create a user context.

        If the context doesn't exist in Redis, creates a new one with
        the current date and IDLE state, then persists it.

        Args:
            user_id: Telegram user ID

        Returns:
            UserContext object for the specified user
        """
        data = self._redis.get(self._key(user_id))
        if data is not None:
            return self._deserialize(data)

        now = datetime.now()
        ctx = UserContext(
            user_id=user_id,
            state=UserState.IDLE,
            active_date=_get_today_date(),
            calendar_month=now.month,
            calendar_year=now.year,
            task_page=0,
            last_message_id=None,
        )
        self._save(ctx)
        return ctx

    def update_context(self, user_id: int, **kwargs: Any) -> None:
        """
        Update specific fields of a user's context.

        Loads the current context from Redis, applies the updates,
        and saves it back.

        Args:
            user_id: Telegram user ID
            **kwargs: Fields to update (e.g., state=UserState.WAITING_RATING)

        Example:
            manager.update_context(123, state=UserState.TASKS_VIEW, task_page=1)
        """
        ctx = self.get_context(user_id)
        for key, value in kwargs.items():
            if hasattr(ctx, key):
                setattr(ctx, key, value)
        self._save(ctx)

    def reset_context(self, user_id: int) -> None:
        """
        Reset user context to IDLE state.

        Resets the state to IDLE and clears task_page and last_message_id,
        but preserves active_date and calendar settings.

        Args:
            user_id: Telegram user ID
        """
        ctx = self.get_context(user_id)
        ctx.state = UserState.IDLE
        ctx.task_page = 0
        ctx.last_message_id = None
        self._save(ctx)

    def set_active_date(self, user_id: int, date: str) -> None:
        """
        Set the active date for a user.

        Args:
            user_id: Telegram user ID
            date: Date string in format DD-MMM-YYYY (e.g., 11-Oct-2025)
        """
        ctx = self.get_context(user_id)
        ctx.active_date = date
        self._save(ctx)
