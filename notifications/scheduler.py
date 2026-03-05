"""Scheduler for the notifications service."""

import json
import logging
import threading
import time
import urllib.request
import urllib.parse
from datetime import datetime, timedelta, timezone
from typing import Any, Dict, Optional

import grpc

from notifications.config import (
    BOT_TOKEN,
    SCHEDULER_INTERVAL_SECONDS,
    TIMEZONE_OFFSET_HOURS,
    CORE_GRPC_HOST,
    CORE_GRPC_PORT,
)
from notifications.db import get_due_reminders, update_next_fire
from proto import notes_pb2, notes_pb2_grpc

logger = logging.getLogger(__name__)

_core_stub = None


def _get_core_stub():
    global _core_stub
    if _core_stub is None:
        channel = grpc.insecure_channel(f"{CORE_GRPC_HOST}:{CORE_GRPC_PORT}")
        _core_stub = notes_pb2_grpc.NotesServiceStub(channel)
    return _core_stub


def _get_today_date_str() -> str:
    """Return today's date in DD-MMM-YYYY format via core gRPC."""
    try:
        response = _get_core_stub().GetTodayDate(notes_pb2.Empty(), timeout=5)
        return response.date
    except Exception as e:
        logger.error(f"Failed to get today date from core: {e}")
        # Fallback: compute locally
        local_now = datetime.now(timezone.utc) + timedelta(hours=TIMEZONE_OFFSET_HOURS)
        return local_now.strftime("%d-%b-%Y")


def _add_task_to_today(title: str, today_date: str) -> None:
    """Add a task to today's note via core gRPC."""
    try:
        stub = _get_core_stub()
        stub.EnsureNote(notes_pb2.DateRequest(date=today_date), timeout=5)
        stub.AddTask(
            notes_pb2.AddTaskRequest(date=today_date, task_text=title), timeout=5
        )
        logger.info(f"Added task '{title}' to note {today_date}")
    except Exception as e:
        logger.error(f"Failed to add task to core: {e}")


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


def _build_keyboard(reminder_id: int, create_task: bool = False, today_date: str = "") -> Dict:
    done_cb = (
        f"reminder:done:{reminder_id}:1:{today_date}"
        if create_task and today_date
        else f"reminder:done:{reminder_id}:0"
    )
    return {
        "inline_keyboard": [
            [
                {"text": "✅ Принято", "callback_data": done_cb},
            ],
            [
                {"text": "+1 ч", "callback_data": f"reminder:postpone_hours:1:{reminder_id}"},
                {"text": "+3 ч", "callback_data": f"reminder:postpone_hours:3:{reminder_id}"},
            ],
            [
                {"text": "+1 д", "callback_data": f"reminder:postpone:1:{reminder_id}"},
                {"text": "+3 д", "callback_data": f"reminder:postpone:3:{reminder_id}"},
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
                create_task = reminder.get("create_task", False)

                # If create_task flag is set, add task to today's note
                today_date = ""
                if create_task:
                    today_date = _get_today_date_str()
                    _add_task_to_today(title, today_date)

                # Send notification
                keyboard = _build_keyboard(rid, create_task=create_task, today_date=today_date)
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
