import asyncio
import logging

from telegram import Update
from telegram.ext import ContextTypes

from ..config import ROOT_ID
from ..grpc_client import core_client
from ..keyboards.main_menu import get_main_menu_keyboard
from ..states import state_manager
from ..utils import escape_markdown_v2
from ..whisper_client import whisper_client, WhisperUnavailableError

logger = logging.getLogger(__name__)


async def handle_voice_message(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    if not update or not update.effective_message:
        return
    user_id = update.effective_user.id if update.effective_user else None
    if ROOT_ID and user_id != ROOT_ID:
        return

    voice = update.effective_message.voice
    video_note = update.effective_message.video_note
    if voice:
        file_id, fmt = voice.file_id, "ogg"
    elif video_note:
        file_id, fmt = video_note.file_id, "mp4"
    else:
        return

    status_msg = await update.effective_message.reply_text("⏳ Транскрибирую...")

    try:
        tg_file = await context.bot.get_file(file_id)
        audio_bytes = await tg_file.download_as_bytearray()
        data = bytes(audio_bytes)

        loop = asyncio.get_event_loop()
        text = await loop.run_in_executor(None, whisper_client.transcribe, data, fmt)

        if not text:
            await status_msg.edit_text(
                "⚠️ Не удалось распознать речь\\.", parse_mode="MarkdownV2"
            )
            return

        ctx = state_manager.get_context(user_id)
        active_date = ctx.active_date
        core_client.append_to_note(active_date, text)

        escaped = escape_markdown_v2(text)
        keyboard = get_main_menu_keyboard(active_date)
        await status_msg.edit_text(
            f"🎙 Добавлено в заметку:\n\n_{escaped}_",
            parse_mode="MarkdownV2",
            reply_markup=keyboard,
        )

    except WhisperUnavailableError:
        await status_msg.edit_text(
            "⏳ Сервис распознавания ещё запускается\\. Попробуйте через несколько секунд\\.",
            parse_mode="MarkdownV2",
        )
    except Exception as e:
        logger.error(f"Voice handler error: {e}")
        await status_msg.edit_text(
            "❌ Ошибка при обработке голосового сообщения\\.", parse_mode="MarkdownV2"
        )
