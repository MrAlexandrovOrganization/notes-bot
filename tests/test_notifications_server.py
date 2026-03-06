"""Tests for notifications/server.py — _compute_initial_next_fire and gRPC servicer RPCs.

Strategy: patch underlying DB functions and call servicer methods directly
(no actual gRPC transport or database needed).
"""

import json
from datetime import datetime, timezone, timedelta
from unittest.mock import MagicMock, patch

import grpc

from notifications.server import NotificationsServicer, _compute_initial_next_fire


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_context():
    return MagicMock()


def _make_servicer():
    return NotificationsServicer()


def _future_iso(hours=12):
    return (datetime.now(timezone.utc) + timedelta(hours=hours)).isoformat()


def _db_row(
    rid=1,
    user_id=123,
    title="Test",
    schedule_type="daily",
    params=None,
    next_fire=None,
    create_task=False,
):
    return {
        "id": rid,
        "user_id": user_id,
        "title": title,
        "schedule_type": schedule_type,
        "schedule_params": params or {"hour": 9, "minute": 0, "tz_offset": 3},
        "next_fire_at": next_fire or datetime(2026, 3, 4, 6, 0, 0, tzinfo=timezone.utc),
        "is_active": True,
        "create_task": create_task,
    }


# ---------------------------------------------------------------------------
# _compute_initial_next_fire
# ---------------------------------------------------------------------------


class TestComputeInitialNextFire:
    def test_once_converts_local_time_to_utc(self):
        """2026-04-01 09:00 +3 → 2026-04-01 06:00:00 UTC."""
        params = {"date": "2026-04-01", "hour": 9, "minute": 0, "tz_offset": 3}
        result = _compute_initial_next_fire("once", params)
        assert "2026-04-01T06:00:00" in result

    def test_once_with_zero_tz_offset(self):
        """2026-04-01 14:30 +0 → 2026-04-01 14:30:00 UTC."""
        params = {"date": "2026-04-01", "hour": 14, "minute": 30, "tz_offset": 0}
        result = _compute_initial_next_fire("once", params)
        assert "2026-04-01T14:30:00" in result

    def test_once_without_tz_offset_uses_config_default_zero(self):
        """No tz_offset → uses TIMEZONE_OFFSET_HOURS=0 from notifications/config."""
        params = {"date": "2026-05-01", "hour": 10, "minute": 0}
        result = _compute_initial_next_fire("once", params)
        assert "2026-05-01T10:00:00" in result

    def test_once_with_invalid_date_falls_back_to_now(self):
        params = {"date": "not-a-date", "hour": 9, "minute": 0, "tz_offset": 3}
        before = datetime.now(timezone.utc)
        result = _compute_initial_next_fire("once", params)
        after = datetime.now(timezone.utc)
        result_dt = datetime.fromisoformat(result)
        if result_dt.tzinfo is None:
            result_dt = result_dt.replace(tzinfo=timezone.utc)
        assert before <= result_dt <= after

    def test_once_with_missing_date_falls_back_to_now(self):
        params = {"hour": 9, "minute": 0, "tz_offset": 3}  # no "date" key
        before = datetime.now(timezone.utc)
        result = _compute_initial_next_fire("once", params)
        after = datetime.now(timezone.utc)
        result_dt = datetime.fromisoformat(result)
        if result_dt.tzinfo is None:
            result_dt = result_dt.replace(tzinfo=timezone.utc)
        assert before <= result_dt <= after

    def test_daily_delegates_to_compute_next_fire(self):
        fixed = datetime(2026, 3, 4, 6, 0, 0, tzinfo=timezone.utc)
        with patch("notifications.server._compute_next_fire", return_value=fixed):
            result = _compute_initial_next_fire("daily", {"hour": 9, "minute": 0})
        assert "2026-03-04T06:00:00" in result

    def test_when_compute_next_fire_returns_none_falls_back_to_now(self):
        with patch("notifications.server._compute_next_fire", return_value=None):
            before = datetime.now(timezone.utc)
            result = _compute_initial_next_fire("weekly", {"days": [0]})
            after = datetime.now(timezone.utc)
        result_dt = datetime.fromisoformat(result)
        if result_dt.tzinfo is None:
            result_dt = result_dt.replace(tzinfo=timezone.utc)
        assert before <= result_dt <= after


# ---------------------------------------------------------------------------
# CreateReminder
# ---------------------------------------------------------------------------


class TestCreateReminder:
    def _make_request(
        self, schedule_type="daily", params=None, user_id=123, create_task=False
    ):
        req = MagicMock()
        req.user_id = user_id
        req.title = "Standup"
        req.schedule_type = schedule_type
        req.schedule_params_json = json.dumps(
            params or {"hour": 9, "minute": 0, "tz_offset": 3}
        )
        req.create_task = create_task
        return req

    def test_valid_request_returns_success(self):
        servicer = _make_servicer()
        ctx = _make_context()
        row = _db_row()
        with (
            patch(
                "notifications.server._compute_initial_next_fire",
                return_value=_future_iso(),
            ),
            patch("notifications.server.create_reminder", return_value=row),
        ):
            response = servicer.CreateReminder(self._make_request(), ctx)
        assert response.success is True
        ctx.set_code.assert_not_called()

    def test_returned_reminder_has_correct_fields(self):
        servicer = _make_servicer()
        ctx = _make_context()
        row = _db_row(rid=5, user_id=123, title="Standup", schedule_type="daily")
        with (
            patch(
                "notifications.server._compute_initial_next_fire",
                return_value=_future_iso(),
            ),
            patch("notifications.server.create_reminder", return_value=row),
        ):
            response = servicer.CreateReminder(self._make_request(), ctx)
        assert response.reminder.id == 5
        assert response.reminder.title == "Standup"
        assert response.reminder.schedule_type == "daily"

    def test_invalid_json_params_returns_invalid_argument(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.schedule_params_json = "{{bad json}}"
        response = servicer.CreateReminder(req, ctx)
        assert response.success is False
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INVALID_ARGUMENT)

    def test_empty_params_json_uses_defaults(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.user_id = 123
        req.title = "Daily"
        req.schedule_type = "daily"
        req.schedule_params_json = ""
        row = _db_row(params={})
        with (
            patch(
                "notifications.server._compute_initial_next_fire",
                return_value=_future_iso(),
            ),
            patch("notifications.server.create_reminder", return_value=row),
        ):
            response = servicer.CreateReminder(req, ctx)
        assert response.success is True

    def test_past_next_fire_returns_invalid_argument(self):
        servicer = _make_servicer()
        ctx = _make_context()
        past = datetime(2020, 1, 1, 9, 0, 0, tzinfo=timezone.utc).isoformat()
        with patch(
            "notifications.server._compute_initial_next_fire", return_value=past
        ):
            response = servicer.CreateReminder(self._make_request(), ctx)
        assert response.success is False
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INVALID_ARGUMENT)
        assert "past" in ctx.set_details.call_args[0][0].lower()

    def test_db_exception_returns_internal_error(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with (
            patch(
                "notifications.server._compute_initial_next_fire",
                return_value=_future_iso(),
            ),
            patch(
                "notifications.server.create_reminder",
                side_effect=Exception("conn refused"),
            ),
        ):
            response = servicer.CreateReminder(self._make_request(), ctx)
        assert response.success is False
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INTERNAL)

    def test_once_schedule_type_with_future_date(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = self._make_request(
            schedule_type="once",
            params={"date": "2030-01-01", "hour": 9, "minute": 0, "tz_offset": 3},
        )
        row = _db_row(schedule_type="once")
        future = datetime(2030, 1, 1, 6, 0, 0, tzinfo=timezone.utc).isoformat()
        with (
            patch(
                "notifications.server._compute_initial_next_fire", return_value=future
            ),
            patch("notifications.server.create_reminder", return_value=row),
        ):
            response = servicer.CreateReminder(req, ctx)
        assert response.success is True


# ---------------------------------------------------------------------------
# ListReminders
# ---------------------------------------------------------------------------


class TestListReminders:
    def test_returns_reminders_for_user(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.user_id = 123
        rows = [_db_row(rid=1, title="A"), _db_row(rid=2, title="B")]
        with patch("notifications.server.list_reminders", return_value=rows):
            response = servicer.ListReminders(req, ctx)
        assert len(response.reminders) == 2
        titles = {r.title for r in response.reminders}
        assert titles == {"A", "B"}

    def test_returns_empty_list_when_no_reminders(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.user_id = 999
        with patch("notifications.server.list_reminders", return_value=[]):
            response = servicer.ListReminders(req, ctx)
        assert len(response.reminders) == 0

    def test_reminder_proto_fields_populated(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.user_id = 123
        rows = [_db_row(rid=7, user_id=123, title="Weekly", schedule_type="weekly")]
        with patch("notifications.server.list_reminders", return_value=rows):
            response = servicer.ListReminders(req, ctx)
        r = response.reminders[0]
        assert r.id == 7
        assert r.user_id == 123
        assert r.schedule_type == "weekly"
        assert r.is_active is True

    def test_db_exception_returns_internal_error(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.user_id = 123
        with patch(
            "notifications.server.list_reminders", side_effect=Exception("DB error")
        ):
            response = servicer.ListReminders(req, ctx)
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INTERNAL)
        assert len(response.reminders) == 0


# ---------------------------------------------------------------------------
# DeleteReminder
# ---------------------------------------------------------------------------


class TestDeleteReminder:
    def test_deletes_existing_reminder(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.reminder_id = 1
        req.user_id = 123
        with patch("notifications.server.delete_reminder", return_value=True):
            response = servicer.DeleteReminder(req, ctx)
        assert response.success is True

    def test_nonexistent_reminder_returns_false(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.reminder_id = 999
        req.user_id = 123
        with patch("notifications.server.delete_reminder", return_value=False):
            response = servicer.DeleteReminder(req, ctx)
        assert response.success is False

    def test_correct_ids_passed_to_db(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.reminder_id = 42
        req.user_id = 777
        with patch(
            "notifications.server.delete_reminder", return_value=True
        ) as mock_del:
            servicer.DeleteReminder(req, ctx)
        mock_del.assert_called_once_with(42, 777)

    def test_db_exception_returns_internal_error(self):
        servicer = _make_servicer()
        ctx = _make_context()
        req = MagicMock()
        req.reminder_id = 1
        req.user_id = 123
        with patch(
            "notifications.server.delete_reminder", side_effect=Exception("DB error")
        ):
            response = servicer.DeleteReminder(req, ctx)
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INTERNAL)
        assert response.success is False


# ---------------------------------------------------------------------------
# PostponeReminder
# ---------------------------------------------------------------------------


class TestPostponeReminder:
    def _make_request(
        self,
        reminder_id=1,
        user_id=123,
        postpone_days=0,
        target_date="",
        postpone_hours=0,
    ):
        req = MagicMock()
        req.reminder_id = reminder_id
        req.user_id = user_id
        req.postpone_days = postpone_days
        req.target_date = target_date
        req.postpone_hours = postpone_hours
        return req

    def test_postpone_by_positive_days(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            response = servicer.PostponeReminder(
                self._make_request(postpone_days=3), ctx
            )
        assert response.success is True
        stored_dt = datetime.fromisoformat(mock_set.call_args[0][2])
        if stored_dt.tzinfo is None:
            stored_dt = stored_dt.replace(tzinfo=timezone.utc)
        delta = stored_dt - datetime.now(timezone.utc)
        # Should be approximately 3 days
        assert timedelta(days=2, hours=23) <= delta <= timedelta(days=3, hours=1)

    def test_postpone_zero_days_defaults_to_one_day(self):
        """postpone_days=0 is treated as 1 to avoid no-op postpones."""
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            response = servicer.PostponeReminder(
                self._make_request(postpone_days=0), ctx
            )
        assert response.success is True
        stored_dt = datetime.fromisoformat(mock_set.call_args[0][2])
        if stored_dt.tzinfo is None:
            stored_dt = stored_dt.replace(tzinfo=timezone.utc)
        delta = stored_dt - datetime.now(timezone.utc)
        assert timedelta(hours=22) <= delta <= timedelta(hours=26)

    def test_postpone_to_target_date(self):
        """target_date sets next_fire to 09:00 on that date in config timezone (UTC+0)."""
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            response = servicer.PostponeReminder(
                self._make_request(target_date="2026-04-15"), ctx
            )
        assert response.success is True
        stored = mock_set.call_args[0][2]
        assert "2026-04-15T09:00:00" in stored

    def test_target_date_takes_precedence_over_postpone_days(self):
        """When target_date is set, postpone_days is ignored."""
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            servicer.PostponeReminder(
                self._make_request(postpone_days=7, target_date="2026-06-01"), ctx
            )
        stored = mock_set.call_args[0][2]
        assert "2026-06-01T09:00:00" in stored

    def test_correct_ids_passed_to_db(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            servicer.PostponeReminder(
                self._make_request(reminder_id=55, user_id=888, postpone_days=1), ctx
            )
        call_args = mock_set.call_args[0]
        assert call_args[0] == 55
        assert call_args[1] == 888

    def test_db_exception_returns_internal_error(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", side_effect=Exception("DB error")
        ):
            response = servicer.PostponeReminder(
                self._make_request(postpone_days=1), ctx
            )
        ctx.set_code.assert_called_once_with(grpc.StatusCode.INTERNAL)
        assert response.success is False

    def test_returns_false_when_reminder_not_found(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch("notifications.server.set_next_fire_at", return_value=False):
            response = servicer.PostponeReminder(
                self._make_request(postpone_days=1), ctx
            )
        assert response.success is False

    def test_postpone_by_hours(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch(
            "notifications.server.set_next_fire_at", return_value=True
        ) as mock_set:
            response = servicer.PostponeReminder(
                self._make_request(postpone_hours=3), ctx
            )
        assert response.success is True
        stored_dt = datetime.fromisoformat(mock_set.call_args[0][2])
        if stored_dt.tzinfo is None:
            stored_dt = stored_dt.replace(tzinfo=timezone.utc)
        delta = stored_dt - datetime.now(timezone.utc)
        assert timedelta(hours=2, minutes=55) <= delta <= timedelta(hours=3, minutes=5)

    def test_response_contains_next_fire_at(self):
        servicer = _make_servicer()
        ctx = _make_context()
        with patch("notifications.server.set_next_fire_at", return_value=True):
            response = servicer.PostponeReminder(
                self._make_request(postpone_days=1), ctx
            )
        assert response.success is True
        assert response.reminder.next_fire_at != ""
        assert response.reminder.id == 1
