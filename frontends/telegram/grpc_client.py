"""gRPC client for communicating with the core service."""

import logging
import os
from typing import Any, Dict, List, Optional, Set

import grpc

from proto import notes_pb2, notes_pb2_grpc

logger = logging.getLogger(__name__)


class CoreClient:
    """Client wrapping all core service gRPC calls."""

    def __init__(self, host: str, port: str):
        self._channel = grpc.insecure_channel(f"{host}:{port}")
        self._stub = notes_pb2_grpc.NotesServiceStub(self._channel)

    def get_today_date(self) -> str:
        response = self._stub.GetTodayDate(notes_pb2.Empty())
        return response.date

    def get_existing_dates(self) -> Set[str]:
        response = self._stub.GetExistingDates(notes_pb2.Empty())
        return set(response.dates)

    def ensure_note(self, date: str) -> bool:
        response = self._stub.EnsureNote(notes_pb2.DateRequest(date=date))
        return response.success

    def get_note(self, date: str) -> Optional[str]:
        try:
            response = self._stub.GetNote(notes_pb2.DateRequest(date=date))
            return response.content
        except grpc.RpcError as e:
            if e.code() == grpc.StatusCode.NOT_FOUND:
                return None
            raise

    def get_rating(self, date: str) -> Optional[int]:
        response = self._stub.GetRating(notes_pb2.DateRequest(date=date))
        if response.has_rating:
            return response.rating
        return None

    def update_rating(self, date: str, rating: int) -> bool:
        response = self._stub.UpdateRating(
            notes_pb2.UpdateRatingRequest(date=date, rating=rating)
        )
        return response.success

    def get_tasks(self, date: str) -> List[Dict[str, Any]]:
        response = self._stub.GetTasks(notes_pb2.DateRequest(date=date))
        return [
            {
                "text": t.text,
                "completed": t.completed,
                "index": t.index,
                "line_number": t.line_number,
            }
            for t in response.tasks
        ]

    def toggle_task(self, date: str, task_index: int) -> bool:
        response = self._stub.ToggleTask(
            notes_pb2.ToggleTaskRequest(date=date, task_index=task_index)
        )
        return response.success

    def add_task(self, date: str, task_text: str) -> bool:
        response = self._stub.AddTask(
            notes_pb2.AddTaskRequest(date=date, task_text=task_text)
        )
        return response.success

    def append_to_note(self, date: str, text: str) -> bool:
        response = self._stub.AppendToNote(
            notes_pb2.AppendRequest(date=date, text=text)
        )
        return response.success


_host = os.getenv("CORE_GRPC_HOST", "localhost")
_port = os.getenv("CORE_GRPC_PORT", "50051")
core_client = CoreClient(_host, _port)
