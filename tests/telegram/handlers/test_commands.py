"""Tests for frontends/telegram/handlers/commands.py."""

from unittest.mock import MagicMock, patch


from tests.telegram.conftest import make_text_update, ROOT_USER_ID


class TestCmdStart:
    async def test_unauthorized_user_gets_rejection(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=9999)

        with patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID):
            await cmd_start(update, MagicMock())

        update.message.reply_text.assert_called_once()
        call_args = update.message.reply_text.call_args[0][0]
        assert "Unauthorized" in call_args

    async def test_authorized_user_gets_welcome_message(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=ROOT_USER_ID)
        mock_context = MagicMock()
        mock_context.active_date = "04-Mar-2026"

        with (
            patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.commands.state_manager") as mock_sm,
        ):
            mock_sm.get_context.return_value = mock_context
            await cmd_start(update, MagicMock())

        update.message.reply_text.assert_called_once()

    async def test_authorized_user_calls_get_context(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=ROOT_USER_ID)
        mock_context = MagicMock()
        mock_context.active_date = "04-Mar-2026"

        with (
            patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.commands.state_manager") as mock_sm,
        ):
            mock_sm.get_context.return_value = mock_context
            await cmd_start(update, MagicMock())

        mock_sm.get_context.assert_called_once_with(ROOT_USER_ID)

    async def test_welcome_message_contains_active_date(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=ROOT_USER_ID)
        mock_context = MagicMock()
        mock_context.active_date = "04-Mar-2026"

        with (
            patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.commands.state_manager") as mock_sm,
        ):
            mock_sm.get_context.return_value = mock_context
            await cmd_start(update, MagicMock())

        call_args = update.message.reply_text.call_args[0][0]
        # active_date is escaped in the message
        assert "04" in call_args

    async def test_welcome_message_has_keyboard(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=ROOT_USER_ID)
        mock_context = MagicMock()
        mock_context.active_date = "04-Mar-2026"

        with (
            patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.commands.state_manager") as mock_sm,
        ):
            mock_sm.get_context.return_value = mock_context
            await cmd_start(update, MagicMock())

        call_kwargs = update.message.reply_text.call_args[1]
        assert "reply_markup" in call_kwargs

    async def test_no_message_returns_early(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = MagicMock()
        update.message = None
        update.effective_user = MagicMock()

        with patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID):
            await cmd_start(update, MagicMock())

    async def test_no_effective_user_returns_early(self):
        from frontends.telegram.handlers.commands import cmd_start

        update = MagicMock()
        update.message = MagicMock()
        update.effective_user = None

        with patch("frontends.telegram.handlers.commands.ROOT_ID", ROOT_USER_ID):
            await cmd_start(update, MagicMock())

    async def test_root_id_none_allows_all_users(self):
        """When ROOT_ID is None, all users should be allowed."""
        from frontends.telegram.handlers.commands import cmd_start

        update = make_text_update("", user_id=9999)
        mock_context = MagicMock()
        mock_context.active_date = "04-Mar-2026"

        with (
            patch("frontends.telegram.handlers.commands.ROOT_ID", None),
            patch("frontends.telegram.handlers.commands.state_manager") as mock_sm,
        ):
            mock_sm.get_context.return_value = mock_context
            await cmd_start(update, MagicMock())

        update.message.reply_text.assert_called_once()
