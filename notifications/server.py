"""gRPC server for the notifications service."""

import json
import logging
from datetime import datetime, timezone, timedelta
from typing import Any, Dict

import grpc

from proto import notifications_pb2, notifications_pb2_grpc
from notifications.config import TIMEZONE_OFFSET_HOURS
from notifications.db import (
    create_reminder,
    list_reminders,
    delete_reminder,
    set_next_fire_at,
)
from notifications.scheduler import _compute_next_fire

logger = logging.getLogger(__name__)


def _row_to_proto(row: Dict[str, Any]) -> notifications_pb2.Reminder:
    next_fire_at = row.get("next_fire_at")
    if next_fire_at is not None:
        next_fire_str = next_fire_at.isoformat() if hasattr(next_fire_at, "isoformat") else str(next_fire_at)
    else:
        next_fire_str = ""
    return notifications_pb2.Reminder(
        id=row["id"],
        user_id=row["user_id"],
        title=row["title"],
        schedule_type=row["schedule_type"],
        schedule_params_json=json.dumps(row["schedule_params"]),
        next_fire_at=next_fire_str,
        is_active=row["is_active"],
    )


def _compute_initial_next_fire(schedule_type: str, params: Dict[str, Any]) -> str:
    tz_hours = params.get("tz_offset", TIMEZONE_OFFSET_HOURS)
    now_utc = datetime.now(timezone.utc)

    if schedule_type == "once":
        date_str = params.get("date", "")
        hour = params.get("hour", 9)
        minute = params.get("minute", 0)
        try:
            local_tz = timezone(timedelta(hours=tz_hours))
            dt = datetime.strptime(date_str, "%Y-%m-%d").replace(
                hour=hour, minute=minute, second=0, tzinfo=local_tz
            )
            return dt.astimezone(timezone.utc).isoformat()
        except Exception:
            return now_utc.isoformat()

    next_dt = _compute_next_fire(schedule_type, params, now_utc)
    if next_dt:
        return next_dt.isoformat()
    return now_utc.isoformat()


class NotificationsServicer(notifications_pb2_grpc.NotificationsServiceServicer):
    def CreateReminder(self, request, context):
        try:
            params = json.loads(request.schedule_params_json) if request.schedule_params_json else {}
        except json.JSONDecodeError:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("Invalid schedule_params_json")
            return notifications_pb2.ReminderResponse(success=False)

        next_fire_at = _compute_initial_next_fire(request.schedule_type, params)

        # Reject if next fire is in the past
        try:
            nf_dt = datetime.fromisoformat(next_fire_at.replace("Z", "+00:00"))
            if nf_dt.tzinfo is None:
                nf_dt = nf_dt.replace(tzinfo=timezone.utc)
            if nf_dt <= datetime.now(timezone.utc):
                context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
                context.set_details("Reminder date is in the past")
                return notifications_pb2.ReminderResponse(success=False)
        except Exception:
            pass

        try:
            row = create_reminder(
                user_id=request.user_id,
                title=request.title,
                schedule_type=request.schedule_type,
                schedule_params=params,
                next_fire_at=next_fire_at,
            )
        except Exception as e:
            logger.error(f"Error creating reminder: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return notifications_pb2.ReminderResponse(success=False)

        return notifications_pb2.ReminderResponse(
            success=True, reminder=_row_to_proto(row)
        )

    def ListReminders(self, request, context):
        try:
            rows = list_reminders(request.user_id)
        except Exception as e:
            logger.error(f"Error listing reminders: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return notifications_pb2.ListRemindersResponse()

        reminders = [_row_to_proto(row) for row in rows]
        return notifications_pb2.ListRemindersResponse(reminders=reminders)

    def DeleteReminder(self, request, context):
        try:
            success = delete_reminder(request.reminder_id, request.user_id)
        except Exception as e:
            logger.error(f"Error deleting reminder: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return notifications_pb2.SuccessResponse(success=False)

        return notifications_pb2.SuccessResponse(success=success)

    def PostponeReminder(self, request, context):
        try:
            if request.target_date:
                # Parse target_date as YYYY-MM-DD
                local_tz = timezone(timedelta(hours=TIMEZONE_OFFSET_HOURS))
                dt = datetime.strptime(request.target_date, "%Y-%m-%d").replace(
                    hour=9, minute=0, second=0, tzinfo=local_tz
                )
                next_fire_str = dt.astimezone(timezone.utc).isoformat()
            else:
                days = request.postpone_days if request.postpone_days > 0 else 1
                next_fire_str = (
                    datetime.now(timezone.utc) + timedelta(days=days)
                ).isoformat()

            success = set_next_fire_at(request.reminder_id, request.user_id, next_fire_str)
        except Exception as e:
            logger.error(f"Error postponing reminder: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return notifications_pb2.SuccessResponse(success=False)

        return notifications_pb2.SuccessResponse(success=success)
