"""gRPC client for communicating with the notifications service."""

import logging
import os
from typing import Any, Dict, List

import grpc

from proto import notifications_pb2, notifications_pb2_grpc

logger = logging.getLogger(__name__)

_GRPC_TIMEOUT = 10


class NotificationsClient:
    """Client wrapping all notifications service gRPC calls."""

    def __init__(self, host: str, port: str):
        self._channel = grpc.insecure_channel(f"{host}:{port}")
        self._stub = notifications_pb2_grpc.NotificationsServiceStub(self._channel)

    def close(self) -> None:
        self._channel.close()

    def create_reminder(
        self,
        user_id: int,
        title: str,
        schedule_type: str,
        schedule_params_json: str,
    ) -> Dict[str, Any]:
        response = self._stub.CreateReminder(
            notifications_pb2.CreateReminderRequest(
                user_id=user_id,
                title=title,
                schedule_type=schedule_type,
                schedule_params_json=schedule_params_json,
            ),
            timeout=_GRPC_TIMEOUT,
        )
        if not response.success:
            return {}
        r = response.reminder
        return {
            "id": r.id,
            "user_id": r.user_id,
            "title": r.title,
            "schedule_type": r.schedule_type,
            "schedule_params_json": r.schedule_params_json,
            "next_fire_at": r.next_fire_at,
            "is_active": r.is_active,
        }

    def list_reminders(self, user_id: int) -> List[Dict[str, Any]]:
        response = self._stub.ListReminders(
            notifications_pb2.ListRemindersRequest(user_id=user_id),
            timeout=_GRPC_TIMEOUT,
        )
        return [
            {
                "id": r.id,
                "user_id": r.user_id,
                "title": r.title,
                "schedule_type": r.schedule_type,
                "schedule_params_json": r.schedule_params_json,
                "next_fire_at": r.next_fire_at,
                "is_active": r.is_active,
            }
            for r in response.reminders
        ]

    def delete_reminder(self, reminder_id: int, user_id: int) -> bool:
        response = self._stub.DeleteReminder(
            notifications_pb2.DeleteReminderRequest(
                reminder_id=reminder_id, user_id=user_id
            ),
            timeout=_GRPC_TIMEOUT,
        )
        return response.success

    def postpone_reminder(
        self,
        reminder_id: int,
        user_id: int,
        postpone_days: int = 0,
        target_date: str = "",
    ) -> bool:
        response = self._stub.PostponeReminder(
            notifications_pb2.PostponeReminderRequest(
                reminder_id=reminder_id,
                user_id=user_id,
                postpone_days=postpone_days,
                target_date=target_date,
            ),
            timeout=_GRPC_TIMEOUT,
        )
        return response.success


_host = os.getenv("NOTIFICATIONS_GRPC_HOST", "localhost")
_port = os.getenv("NOTIFICATIONS_GRPC_PORT", "50052")
notifications_client = NotificationsClient(_host, _port)
