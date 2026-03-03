"""Scheduler for the notifications service."""

import json
import logging
import threading
import time
import urllib.request
import urllib.parse
from datetime import datetime, timedelta, timezone
from typing import Any, Dict, Optional

from notifications.config import (
    BOT_TOKEN,
    SCHEDULER_INTERVAL_SECONDS,
    TIMEZONE_OFFSET_HOURS,
)
from notifications.db import get_due_reminders, update_next_fire

logger = logging.getLogger(__name__)


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)


def _compute_next_fire(
    schedule_type: str, params: Dict[str, Any], after_utc: datetime
) -> Optional[datetime]:
    """Compute the next fire datetime (UTC) for a given schedule."""
    tz_hours = params.get("tz_offset", TIMEZONE_OFFSET_HOURS)
    tz_offset = timezone(timedelta(hours=tz_hours))
    after_local = after_utc.astimezone(tz_offset)

    hour = params.get("hour", 9)
    minute = params.get("minute", 0)

    if schedule_type == "once":
        return None  # Already fired, deactivate

    elif schedule_type == "daily":
        candidate = after_local.replace(hour=hour, minute=minute, second=0, microsecond=0)
        if candidate <= after_local:
            candidate += timedelta(days=1)
        return candidate.astimezone(timezone.utc)

    elif schedule_type == "weekly":
        days = params.get("days", [0])
        candidate = after_local.replace(hour=hour, minute=minute, second=0, microsecond=0)
        if candidate <= after_local:
            candidate += timedelta(days=1)
        for _ in range(7):
            if candidate.weekday() in days:
                return candidate.astimezone(timezone.utc)
            candidate += timedelta(days=1)
        return None

    elif schedule_type == "monthly":
        day_of_month = params.get("day_of_month", 1)
        try:
            candidate = after_local.replace(
                day=day_of_month, hour=hour, minute=minute, second=0, microsecond=0
            )
        except ValueError:
            # day doesn't exist in this month, skip to next
            candidate = None
        if candidate is None or candidate <= after_local:
            month = after_local.month + 1
            year = after_local.year
            if month > 12:
                month = 1
                year += 1
            try:
                candidate = after_local.replace(
                    year=year,
                    month=month,
                    day=day_of_month,
                    hour=hour,
                    minute=minute,
                    second=0,
                    microsecond=0,
                )
            except ValueError:
                return None
        return candidate.astimezone(timezone.utc)

    elif schedule_type == "yearly":
        month = params.get("month", 1)
        day = params.get("day", 1)
        try:
            candidate = after_local.replace(
                month=month, day=day, hour=hour, minute=minute, second=0, microsecond=0
            )
        except ValueError:
            candidate = None
        if candidate is None or candidate <= after_local:
            try:
                candidate = after_local.replace(
                    year=after_local.year + 1,
                    month=month,
                    day=day,
                    hour=hour,
                    minute=minute,
                    second=0,
                    microsecond=0,
                )
            except ValueError:
                return None
        return candidate.astimezone(timezone.utc)

    elif schedule_type == "custom_days":
        interval_days = params.get("interval_days", 1)
        candidate = after_local.replace(hour=hour, minute=minute, second=0, microsecond=0)
        if candidate <= after_local:
            candidate += timedelta(days=interval_days)
        return candidate.astimezone(timezone.utc)

    return None


def _send_telegram_message(chat_id: int, text: str, keyboard: Dict) -> None:
    if not BOT_TOKEN:
        logger.warning("BOT_TOKEN not set, skipping message send")
        return
    url = f"https://api.telegram.org/bot{BOT_TOKEN}/sendMessage"
    payload = json.dumps(
        {
            "chat_id": chat_id,
            "text": text,
            "reply_markup": keyboard,
        }
    ).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            logger.info(f"Sent notification to {chat_id}, status={resp.status}")
    except Exception as e:
        logger.error(f"Failed to send notification to {chat_id}: {e}")


def _build_keyboard(reminder_id: int) -> Dict:
    return {
        "inline_keyboard": [
            [
                {"text": "✅ Принято", "callback_data": f"reminder:done:{reminder_id}"},
            ],
            [
                {"text": "+1 день", "callback_data": f"reminder:postpone:1:{reminder_id}"},
                {"text": "+3 дня", "callback_data": f"reminder:postpone:3:{reminder_id}"},
            ],
            [
                {"text": "📅 Выбрать дату", "callback_data": f"reminder:custom_date:{reminder_id}"},
            ],
        ]
    }


def run_scheduler() -> None:
    logger.info("Scheduler started")
    while True:
        try:
            due = get_due_reminders()
            for reminder in due:
                rid = reminder["id"]
                user_id = reminder["user_id"]
                title = reminder["title"]
                schedule_type = reminder["schedule_type"]
                params = reminder["schedule_params"]

                # Send notification
                keyboard = _build_keyboard(rid)
                _send_telegram_message(user_id, f"🔔 Напоминание: {title}", keyboard)

                # Compute next fire
                next_fire = _compute_next_fire(schedule_type, params, _utc_now())
                next_fire_str = next_fire.isoformat() if next_fire else None
                update_next_fire(rid, next_fire_str)

                logger.info(f"Fired reminder {rid} for user {user_id}, next={next_fire_str}")
        except Exception as e:
            logger.error(f"Scheduler error: {e}")

        time.sleep(SCHEDULER_INTERVAL_SECONDS)


def start_scheduler_thread() -> threading.Thread:
    t = threading.Thread(target=run_scheduler, daemon=True, name="scheduler")
    t.start()
    return t
