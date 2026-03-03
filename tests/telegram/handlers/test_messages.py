"""Tests for frontends/telegram/handlers/messages.py."""

from unittest.mock import MagicMock, patch


from frontends.telegram.states.context import UserState
from tests.telegram.conftest import make_text_update, ROOT_USER_ID


def _make_context(state: UserState, active_date: str = "04-Mar-2026"):
    ctx = MagicMock()
    ctx.state = state
    ctx.active_date = active_date
    return ctx


class TestHandleTextMessageAuth:
    async def test_unauthorized_user_gets_rejection(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("hello", user_id=9999)

        with patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID):
            await handle_text_message(update, MagicMock())

        update.message.reply_text.assert_called_once()
        assert "Unauthorized" in update.message.reply_text.call_args[0][0]

    async def test_no_message_returns_early(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = MagicMock()
        update.message = None
        update.effective_user = MagicMock()

        with patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID):
            await handle_text_message(update, MagicMock())


class TestHandleTextMessageIdleState:
    async def test_idle_appends_to_note(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("some note text", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.IDLE)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.append_to_note.return_value = True
            await handle_text_message(update, MagicMock())

        mock_cc.append_to_note.assert_called_once_with("04-Mar-2026", "some note text")

    async def test_idle_replies_on_success(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("hello", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.IDLE)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.append_to_note.return_value = True
            await handle_text_message(update, MagicMock())

        update.message.reply_text.assert_called()


class TestHandleTextMessageWaitingRating:
    async def test_valid_rating_calls_update_rating(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("7", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.update_rating.return_value = True
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_called_once_with("04-Mar-2026", 7)

    async def test_valid_rating_resets_state_to_idle(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("5", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.update_rating.return_value = True
            await handle_text_message(update, MagicMock())

        mock_sm.update_context.assert_called_with(ROOT_USER_ID, state=UserState.IDLE)

    async def test_rating_too_high_sends_error(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("11", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_not_called()
        update.message.reply_text.assert_called_once()

    async def test_rating_non_numeric_sends_error(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("abc", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_not_called()
        update.message.reply_text.assert_called_once()

    async def test_rating_negative_sends_error(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("-1", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_not_called()

    async def test_boundary_rating_zero_is_valid(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("0", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.update_rating.return_value = True
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_called_once_with("04-Mar-2026", 0)

    async def test_boundary_rating_ten_is_valid(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("10", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_RATING)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.update_rating.return_value = True
            await handle_text_message(update, MagicMock())

        mock_cc.update_rating.assert_called_once_with("04-Mar-2026", 10)


class TestHandleTextMessageWaitingNewTask:
    async def test_adds_task_via_core_client(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("Buy milk", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_NEW_TASK)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.add_task.return_value = True
            await handle_text_message(update, MagicMock())

        mock_cc.add_task.assert_called_once_with("04-Mar-2026", "Buy milk")

    async def test_successful_add_sets_tasks_view_state(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("New task", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_NEW_TASK)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.add_task.return_value = True
            await handle_text_message(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.TASKS_VIEW
        )

    async def test_successful_add_sends_confirmation(self):
        from frontends.telegram.handlers.messages import handle_text_message

        update = make_text_update("Task text", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_NEW_TASK)

        with (
            patch("frontends.telegram.handlers.messages.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.messages.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.messages.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx
            mock_cc.add_task.return_value = True
            await handle_text_message(update, MagicMock())

        assert update.message.reply_text.call_count >= 1
