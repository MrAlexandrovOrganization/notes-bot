"""Tests for frontends/telegram/states/manager.py (pure logic, no async)."""

import fakeredis
import pytest
from datetime import datetime
from unittest.mock import patch

from frontends.telegram.states.manager import StateManager
from frontends.telegram.states.context import UserState


@pytest.fixture
def manager():
    """StateManager backed by an isolated in-process fakeredis."""
    return StateManager(redis_client=fakeredis.FakeRedis(decode_responses=True))


class TestGetContext:
    def test_creates_new_context_with_idle_state(self, manager):
        ctx = manager.get_context(1)
        assert ctx.state == UserState.IDLE

    def test_creates_new_context_with_today_date(self):
        with patch(
            "frontends.telegram.states.manager._get_today_date",
            return_value="04-Mar-2026",
        ):
            m = StateManager(redis_client=fakeredis.FakeRedis(decode_responses=True))
            ctx = m.get_context(99)
        assert ctx.active_date == "04-Mar-2026"

    def test_returns_context_with_same_values_on_repeated_calls(self, manager):
        ctx1 = manager.get_context(5)
        ctx2 = manager.get_context(5)
        assert ctx1.user_id == ctx2.user_id
        assert ctx1.state == ctx2.state
        assert ctx1.active_date == ctx2.active_date

    def test_creates_context_with_calendar_month_and_year(self, manager):
        ctx = manager.get_context(10)
        now = datetime.now()
        assert ctx.calendar_month == now.month
        assert ctx.calendar_year == now.year

    def test_creates_context_with_zero_task_page(self, manager):
        ctx = manager.get_context(20)
        assert ctx.task_page == 0

    def test_creates_context_with_no_last_message_id(self, manager):
        ctx = manager.get_context(30)
        assert ctx.last_message_id is None

    def test_different_users_have_independent_contexts(self, manager):
        manager.get_context(100)
        manager.get_context(200)
        manager.update_context(100, state=UserState.WAITING_RATING)
        assert manager.get_context(200).state == UserState.IDLE

    def test_returns_correct_user_id(self, manager):
        ctx = manager.get_context(777)
        assert ctx.user_id == 777


class TestUpdateContext:
    def test_updates_state_field(self, manager):
        manager.get_context(1)
        manager.update_context(1, state=UserState.WAITING_RATING)
        assert manager.get_context(1).state == UserState.WAITING_RATING

    def test_updates_multiple_fields(self, manager):
        manager.get_context(1)
        manager.update_context(1, task_page=3, state=UserState.TASKS_VIEW)
        ctx = manager.get_context(1)
        assert ctx.task_page == 3
        assert ctx.state == UserState.TASKS_VIEW

    def test_ignores_unknown_fields(self, manager):
        manager.get_context(1)
        # Should not raise AttributeError
        manager.update_context(1, nonexistent_field="value")

    def test_creates_context_if_missing(self, manager):
        manager.update_context(999, state=UserState.CALENDAR_VIEW)
        assert manager.get_context(999).state == UserState.CALENDAR_VIEW

    def test_updates_active_date(self, manager):
        manager.get_context(1)
        manager.update_context(1, active_date="01-Jan-2026")
        assert manager.get_context(1).active_date == "01-Jan-2026"

    def test_persists_across_get_calls(self, manager):
        manager.update_context(42, state=UserState.TASKS_VIEW, task_page=2)
        ctx = manager.get_context(42)
        assert ctx.state == UserState.TASKS_VIEW
        assert ctx.task_page == 2


class TestResetContext:
    def test_resets_state_to_idle(self, manager):
        manager.update_context(1, state=UserState.WAITING_RATING)
        manager.reset_context(1)
        assert manager.get_context(1).state == UserState.IDLE

    def test_resets_task_page_to_zero(self, manager):
        manager.update_context(1, task_page=5)
        manager.reset_context(1)
        assert manager.get_context(1).task_page == 0

    def test_clears_last_message_id(self, manager):
        manager.update_context(1, last_message_id=123)
        manager.reset_context(1)
        assert manager.get_context(1).last_message_id is None

    def test_preserves_active_date(self, manager):
        manager.update_context(1, active_date="15-Jun-2025")
        manager.reset_context(1)
        assert manager.get_context(1).active_date == "15-Jun-2025"

    def test_preserves_calendar_settings(self, manager):
        manager.update_context(1, calendar_month=6, calendar_year=2025)
        manager.reset_context(1)
        ctx = manager.get_context(1)
        assert ctx.calendar_month == 6
        assert ctx.calendar_year == 2025


class TestSetActiveDate:
    def test_sets_active_date(self, manager):
        manager.get_context(1)
        manager.set_active_date(1, "10-Oct-2025")
        assert manager.get_context(1).active_date == "10-Oct-2025"

    def test_overwrites_existing_active_date(self, manager):
        manager.set_active_date(1, "01-Jan-2025")
        manager.set_active_date(1, "31-Dec-2025")
        assert manager.get_context(1).active_date == "31-Dec-2025"

    def test_creates_context_if_missing(self, manager):
        manager.set_active_date(888, "05-May-2025")
        assert manager.get_context(888).active_date == "05-May-2025"
