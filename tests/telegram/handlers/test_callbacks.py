"""Tests for frontends/telegram/handlers/callbacks.py."""

from unittest.mock import MagicMock, patch


from frontends.telegram.states.context import UserState
from tests.telegram.conftest import (
    make_callback_update,
    ROOT_USER_ID,
)


def _make_context(state: UserState = UserState.IDLE, active_date: str = "04-Mar-2026"):
    ctx = MagicMock()
    ctx.state = state
    ctx.active_date = active_date
    ctx.task_page = 0
    ctx.calendar_month = 3
    ctx.calendar_year = 2026
    return ctx


# ---------------------------------------------------------------------------
# Helpers to set up common patches
# ---------------------------------------------------------------------------

_CALLBACKS_MOD = "frontends.telegram.handlers.callbacks"


def _base_patches(ctx=None):
    """Return a list of patch objects common to most callback tests."""
    if ctx is None:
        ctx = _make_context()
    mock_sm = MagicMock()
    mock_sm.get_context.return_value = ctx
    mock_cc = MagicMock()
    mock_nc = MagicMock()
    return mock_sm, mock_cc, mock_nc


class TestHandleCallbackAuth:
    async def test_unauthorized_user_gets_rejection(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:rating", user_id=9999)

        with patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID):
            await handle_callback(update, MagicMock())

        update.callback_query.edit_message_text.assert_called_once()
        assert "Unauthorized" in update.callback_query.edit_message_text.call_args[0][0]

    async def test_no_query_returns_early(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = MagicMock()
        update.callback_query = None

        with patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID):
            await handle_callback(update, MagicMock())

    async def test_query_answer_is_called(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:rating", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        update.callback_query.answer.assert_called_once()


class TestMenuRatingCallback:
    async def test_sets_state_to_waiting_rating(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:rating", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.WAITING_RATING
        )

    async def test_edits_message_with_rating_prompt(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:rating", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        update.callback_query.edit_message_text.assert_called_once()


class TestMenuTasksCallback:
    async def test_calls_ensure_note(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:tasks", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.ensure_note.assert_called_once_with("04-Mar-2026")

    async def test_calls_get_tasks(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:tasks", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.get_tasks.assert_called_once_with("04-Mar-2026")

    async def test_sets_state_to_tasks_view(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:tasks", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.TASKS_VIEW, task_page=0
        )


class TestMenuNoteCallback:
    async def test_calls_get_note(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:note", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.ensure_note.return_value = True
        mock_cc.get_note.return_value = "Some note content"
        mock_cc.get_rating.return_value = None

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.get_note.assert_called_once_with("04-Mar-2026")

    async def test_get_note_returns_none_shows_error(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:note", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.ensure_note.return_value = True
        mock_cc.get_note.return_value = None

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        update.callback_query.edit_message_text.assert_called_once()


class TestMenuCalendarCallback:
    async def test_calls_get_existing_dates(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:calendar", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.get_existing_dates.assert_called_once()

    async def test_sets_state_to_calendar_view(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("menu:calendar", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.CALENDAR_VIEW
        )


class TestTaskToggleCallback:
    async def test_calls_toggle_task(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:toggle:0", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.TASKS_VIEW)
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.toggle_task.return_value = True
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.toggle_task.assert_called_once_with("04-Mar-2026", 0)

    async def test_re_fetches_tasks_after_toggle(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:toggle:2", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.TASKS_VIEW)
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.toggle_task.return_value = True
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.get_tasks.assert_called_once_with("04-Mar-2026")


class TestTaskAddCallback:
    async def test_sets_state_to_waiting_new_task(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:add", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.WAITING_NEW_TASK
        )

    async def test_shows_cancel_keyboard(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:add", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        call_kwargs = update.callback_query.edit_message_text.call_args[1]
        assert "reply_markup" in call_kwargs


class TestTaskCancelCallback:
    async def test_sets_state_to_tasks_view(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:cancel", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.WAITING_NEW_TASK)
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, state=UserState.TASKS_VIEW
        )


class TestTaskPageCallback:
    async def test_updates_task_page_in_context(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:page:1", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_tasks.return_value = []

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(ROOT_USER_ID, task_page=1)


class TestTaskBackCallback:
    async def test_sets_state_to_idle(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:back", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.TASKS_VIEW)
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(ROOT_USER_ID, state=UserState.IDLE)

    async def test_shows_main_menu(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("task:back", user_id=ROOT_USER_ID)
        ctx = _make_context(UserState.TASKS_VIEW)
        mock_sm, mock_cc, _ = _base_patches(ctx)

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        update.callback_query.edit_message_text.assert_called_once()
        call_kwargs = update.callback_query.edit_message_text.call_args[1]
        assert "reply_markup" in call_kwargs


class TestCalSelectCallback:
    async def test_sets_active_date(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:select:04-Mar-2026", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.ensure_note.return_value = True

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.set_active_date.assert_called_once_with(ROOT_USER_ID, "04-Mar-2026")

    async def test_resets_state_to_idle(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:select:04-Mar-2026", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.ensure_note.return_value = True

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(ROOT_USER_ID, state=UserState.IDLE)

    async def test_shows_main_menu(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:select:04-Mar-2026", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.ensure_note.return_value = True

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        update.callback_query.edit_message_text.assert_called_once()


class TestCalTodayCallback:
    async def test_calls_get_today_date(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:today", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_today_date.return_value = "04-Mar-2026"
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_cc.get_today_date.assert_called_once()

    async def test_sets_active_date_to_today(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:today", user_id=ROOT_USER_ID)
        ctx = _make_context()
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_today_date.return_value = "04-Mar-2026"
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.set_active_date.assert_called_once_with(ROOT_USER_ID, "04-Mar-2026")


class TestCalPrevNextCallback:
    async def test_cal_prev_decrements_month(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:prev", user_id=ROOT_USER_ID)
        ctx = _make_context()
        ctx.calendar_month = 3
        ctx.calendar_year = 2026
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, calendar_month=2, calendar_year=2026
        )

    async def test_cal_prev_wraps_january_to_december(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:prev", user_id=ROOT_USER_ID)
        ctx = _make_context()
        ctx.calendar_month = 1
        ctx.calendar_year = 2026
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, calendar_month=12, calendar_year=2025
        )

    async def test_cal_next_increments_month(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:next", user_id=ROOT_USER_ID)
        ctx = _make_context()
        ctx.calendar_month = 3
        ctx.calendar_year = 2026
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, calendar_month=4, calendar_year=2026
        )

    async def test_cal_next_wraps_december_to_january(self):
        from frontends.telegram.handlers.callbacks import handle_callback

        update = make_callback_update("cal:next", user_id=ROOT_USER_ID)
        ctx = _make_context()
        ctx.calendar_month = 12
        ctx.calendar_year = 2025
        mock_sm, mock_cc, _ = _base_patches(ctx)
        mock_cc.get_existing_dates.return_value = set()

        with (
            patch(f"{_CALLBACKS_MOD}.ROOT_ID", ROOT_USER_ID),
            patch(f"{_CALLBACKS_MOD}.state_manager", mock_sm),
            patch(f"{_CALLBACKS_MOD}.core_client", mock_cc),
        ):
            await handle_callback(update, MagicMock())

        mock_sm.update_context.assert_called_with(
            ROOT_USER_ID, calendar_month=1, calendar_year=2026
        )
