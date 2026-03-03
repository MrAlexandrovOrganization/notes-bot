"""Tests for frontends/telegram/handlers/voice.py."""

from unittest.mock import AsyncMock, MagicMock, patch

from tests.telegram.conftest import (
    make_video_note_update,
    make_voice_update,
    ROOT_USER_ID,
)


def _make_context_obj(active_date: str = "04-Mar-2026") -> MagicMock:
    ctx = MagicMock()
    ctx.active_date = active_date
    return ctx


def _make_bot_context() -> MagicMock:
    tg_file = MagicMock()
    tg_file.download_as_bytearray = AsyncMock(return_value=bytearray(b"audio"))
    bot_ctx = MagicMock()
    bot_ctx.bot.get_file = AsyncMock(return_value=tg_file)
    return bot_ctx


class TestVoiceHandlerAuth:
    async def test_unauthorized_user_returns_silently(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=9999)

        with patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID):
            await handle_voice_message(update, MagicMock())

        update.effective_message.reply_text.assert_not_called()

    async def test_no_effective_message_returns_silently(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = MagicMock()
        update.effective_message = None

        await handle_voice_message(update, MagicMock())

    async def test_no_voice_or_video_note_returns_silently(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = MagicMock()
        update.effective_user.id = ROOT_USER_ID
        update.effective_message.voice = None
        update.effective_message.video_note = None
        update.effective_message.reply_text = AsyncMock()

        with patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID):
            await handle_voice_message(update, MagicMock())

        update.effective_message.reply_text.assert_not_called()


class TestVoiceHandlerEmptyTranscription:
    async def test_empty_transcription_sends_warning(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = ""

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
        ):
            await handle_voice_message(update, bot_ctx)

        status_msg.edit_text.assert_called_once()
        call_args = status_msg.edit_text.call_args[0][0]
        assert "распознать" in call_args


class TestVoiceHandlerSuccess:
    async def test_appends_transcribed_text_to_note(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()
        ctx_obj = _make_context_obj("04-Mar-2026")

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = "hello world"

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
            patch("frontends.telegram.handlers.voice.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.voice.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx_obj
            await handle_voice_message(update, bot_ctx)

        mock_cc.append_to_note.assert_called_once_with("04-Mar-2026", "hello world")

    async def test_success_edits_status_with_transcription(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()
        ctx_obj = _make_context_obj()

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = "test transcription"

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
            patch("frontends.telegram.handlers.voice.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.voice.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx_obj
            mock_cc.append_to_note.return_value = True
            await handle_voice_message(update, bot_ctx)

        status_msg.edit_text.assert_called_once()
        call_text = status_msg.edit_text.call_args[0][0]
        assert "Добавлено" in call_text

    async def test_success_reply_includes_keyboard(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()
        ctx_obj = _make_context_obj()

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = "keyboard test"

        mock_keyboard = MagicMock()

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
            patch("frontends.telegram.handlers.voice.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.voice.core_client") as mock_cc,
            patch(
                "frontends.telegram.handlers.voice.get_main_menu_keyboard",
                return_value=mock_keyboard,
            ),
        ):
            mock_sm.get_context.return_value = ctx_obj
            mock_cc.append_to_note.return_value = True
            await handle_voice_message(update, bot_ctx)

        kwargs = status_msg.edit_text.call_args[1]
        assert kwargs.get("reply_markup") is mock_keyboard


class TestVideoNoteHandler:
    async def test_video_note_transcribed_and_appended(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_video_note_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()
        ctx_obj = _make_context_obj("04-Mar-2026")

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = "video note text"

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
            patch("frontends.telegram.handlers.voice.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.voice.core_client") as mock_cc,
        ):
            mock_sm.get_context.return_value = ctx_obj
            await handle_voice_message(update, bot_ctx)

        mock_wc.transcribe.assert_called_once_with(b"audio", "mp4")
        mock_cc.append_to_note.assert_called_once_with("04-Mar-2026", "video note text")

    async def test_video_note_uses_mp4_format(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_video_note_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()
        ctx_obj = _make_context_obj()

        mock_wc = MagicMock()
        mock_wc.transcribe.return_value = "some text"

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
            patch("frontends.telegram.handlers.voice.state_manager") as mock_sm,
            patch("frontends.telegram.handlers.voice.core_client"),
        ):
            mock_sm.get_context.return_value = ctx_obj
            await handle_voice_message(update, bot_ctx)

        _, fmt = mock_wc.transcribe.call_args[0]
        assert fmt == "mp4"


class TestVoiceHandlerWhisperUnavailable:
    async def test_whisper_unavailable_sends_informative_message(self):
        from frontends.telegram.handlers.voice import handle_voice_message
        from frontends.telegram.whisper_client import WhisperUnavailableError

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = _make_bot_context()

        mock_wc = MagicMock()
        mock_wc.transcribe.side_effect = WhisperUnavailableError()

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
            patch("frontends.telegram.handlers.voice.whisper_client", mock_wc),
        ):
            await handle_voice_message(update, bot_ctx)

        status_msg.edit_text.assert_called_once()
        call_text = status_msg.edit_text.call_args[0][0]
        assert "запускается" in call_text


class TestVoiceHandlerException:
    async def test_exception_sends_error_message(self):
        from frontends.telegram.handlers.voice import handle_voice_message

        update = make_voice_update(user_id=ROOT_USER_ID)
        status_msg = AsyncMock()
        update.effective_message.reply_text = AsyncMock(return_value=status_msg)
        bot_ctx = MagicMock()
        bot_ctx.bot.get_file = AsyncMock(side_effect=RuntimeError("network error"))

        with (
            patch("frontends.telegram.handlers.voice.ROOT_ID", ROOT_USER_ID),
        ):
            await handle_voice_message(update, bot_ctx)

        status_msg.edit_text.assert_called_once()
        call_text = status_msg.edit_text.call_args[0][0]
        assert "Ошибка" in call_text
