"""gRPC server for the core notes service."""

import logging

import grpc

from proto import notes_pb2, notes_pb2_grpc
from core.config import DAILY_NOTES_DIR, NOTES_DIR
from core.notes import read_note, create_daily_note_from_template
from core.features.rating import get_rating_impl, update_rating
from core.features.tasks import parse_tasks, toggle_task, add_task
from core.features.calendar_ops import get_existing_dates
from core.utils import get_today_filename

logger = logging.getLogger(__name__)


class NotesServicer(notes_pb2_grpc.NotesServiceServicer):
    def GetTodayDate(self, request: notes_pb2.Empty, context):
        filename = get_today_filename()
        date = filename.replace(".md", "")
        return notes_pb2.DateResponse(date=date)

    def GetExistingDates(self, request: notes_pb2.Empty, context):
        dates = get_existing_dates(NOTES_DIR)
        return notes_pb2.ExistingDatesResponse(dates=list(dates))

    def EnsureNote(self, request: notes_pb2.DateRequest, context):
        date = request.date
        filepath = DAILY_NOTES_DIR / f"{date}.md"
        if not filepath.exists():
            create_daily_note_from_template(filepath, date)
        return notes_pb2.SuccessResponse(success=True)

    def GetNote(self, request: notes_pb2.DateRequest, context):
        content = read_note(f"{request.date}.md")
        if content is None:
            context.set_code(grpc.StatusCode.NOT_FOUND)
            context.set_details(f"Note not found for date: {request.date}")
            return notes_pb2.NoteResponse()
        return notes_pb2.NoteResponse(content=content)

    def GetRating(
        self, request: notes_pb2.DateRequest, context
    ) -> notes_pb2.RatingResponse:
        content = read_note(f"{request.date}.md")
        if content is None:
            return notes_pb2.RatingResponse(has_rating=False, rating=0)
        rating = get_rating_impl(content)
        if rating is None:
            return notes_pb2.RatingResponse(has_rating=False, rating=0)
        return notes_pb2.RatingResponse(has_rating=True, rating=rating)

    def UpdateRating(self, request: notes_pb2.UpdateRatingRequest, context):
        filepath = DAILY_NOTES_DIR / f"{request.date}.md"
        success = update_rating(filepath, request.rating)
        return notes_pb2.SuccessResponse(success=success)

    def GetTasks(self, request: notes_pb2.DateRequest, context):
        content = read_note(f"{request.date}.md")
        if content is None:
            return notes_pb2.TasksResponse(tasks=[])
        raw_tasks = parse_tasks(content)
        tasks = [
            notes_pb2.Task(
                text=t["text"],
                completed=t["completed"],
                index=t["index"],
                line_number=t["line_number"],
            )
            for t in raw_tasks
        ]
        return notes_pb2.TasksResponse(tasks=tasks)

    def ToggleTask(self, request: notes_pb2.ToggleTaskRequest, context):
        filepath = DAILY_NOTES_DIR / f"{request.date}.md"
        success = toggle_task(filepath, request.task_index)
        return notes_pb2.SuccessResponse(success=success)

    def AddTask(self, request: notes_pb2.AddTaskRequest, context):
        filepath = DAILY_NOTES_DIR / f"{request.date}.md"
        success = add_task(filepath, request.task_text)
        return notes_pb2.SuccessResponse(success=success)

    def AppendToNote(self, request: notes_pb2.AppendRequest, context):
        date = request.date
        filepath = DAILY_NOTES_DIR / f"{date}.md"
        if not filepath.exists():
            create_daily_note_from_template(filepath, date)
        try:
            with open(filepath, "a", encoding="utf-8") as f:
                f.write(f"{request.text}\n")
            return notes_pb2.SuccessResponse(success=True)
        except Exception as e:
            logger.error(f"Error appending to note {date}: {e}")
            return notes_pb2.SuccessResponse(success=False)
