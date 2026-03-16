package tghandlers

import (
	"context"
	"fmt"
	"io"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/clients"
	"notes_bot/internal/telemetry"
)

func (a *App) HandleVoiceMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	userID := update.Message.From.ID
	if !a.authorized(userID) {
		return
	}

	chatID := update.Message.Chat.ID

	var fileID, format string
	if update.Message.Voice != nil {
		fileID = update.Message.Voice.FileID
		format = "ogg"
	} else if update.Message.VideoNote != nil {
		fileID = update.Message.VideoNote.FileID
		format = "mp4"
	} else {
		return
	}
	span.SetAttributes(attribute.String("voice.format", format))

	statusMsg, err := tgBot.Send(tgbotapi.NewMessage(chatID, "⏳ Транскрибирую..."))
	if err != nil {
		a.Logger.Error("send status message", zap.Error(err))
		return
	}

	// Download the file
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := tgBot.GetFile(fileConfig)
	if err != nil {
		a.editStatus(tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при загрузке файла.")
		return
	}

	fileURL := tgFile.Link(tgBot.Token)
	audioData, err := downloadFile(ctx, fileURL)
	if err != nil {
		a.editStatus(tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при загрузке файла.")
		return
	}

	text, err := a.Whisper.Transcribe(ctx, audioData, format)
	if err != nil {
		if _, ok := err.(*clients.ServiceUnavailableError); ok {
			a.editStatus(tgBot, chatID, statusMsg.MessageID,
				"⏳ Сервис распознавания ещё запускается. Попробуйте через несколько секунд.")
			return
		}
		a.Logger.Error("transcribe error", zap.Error(err))
		a.editStatus(tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при обработке голосового сообщения.")
		return
	}

	if text == "" {
		a.editStatus(tgBot, chatID, statusMsg.MessageID, "⚠️ Не удалось распознать речь.")
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		a.Logger.Error("get context", zap.Error(err))
		return
	}

	a.Core.AppendToNote(ctx, uc.ActiveDate, text)

	kb := a.getMainMenuKeyboard(ctx)
	edit := tgbotapi.NewEditMessageText(chatID, statusMsg.MessageID,
		fmt.Sprintf("🎙 Добавлено в заметку:\n\n_%s_", text))
	edit.ParseMode = "MarkdownV2"
	edit.ReplyMarkup = &kb
	tgBot.Send(edit)
}

func (a *App) editStatus(tgBot *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = "MarkdownV2"
	tgBot.Send(edit)
}

func downloadFile(ctx context.Context, url string) ([]byte, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
