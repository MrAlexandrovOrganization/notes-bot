package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

func (a *App) HandleVoiceMessage(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if update.Message == nil || update.Message.From == nil {
		return
	}

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

	log := applog.With(ctx, a.Logger)
	statusMsg, err := tgBot.Send(tgbotapi.NewMessage(chatID, "⏳ Транскрибирую..."))
	if err != nil {
		log.Error("send status message", zap.Error(err))
		return
	}

	// Download the file
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := tgBot.GetFile(fileConfig)
	if err != nil {
		a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при загрузке файла.")
		return
	}

	fileURL := tgFile.Link(tgBot.Token)
	rc, err := downloadFile(ctx, fileURL)
	if err != nil {
		a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при загрузке файла.")
		return
	}
	defer rc.Close()

	text, err := a.Whisper.Transcribe(ctx, rc, format, "voice")
	if err != nil {
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID,
				"⏳ Сервис распознавания ещё запускается. Попробуйте через несколько секунд.")
			return
		}
		log.Error("transcribe error", zap.Error(err))
		a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при обработке голосового сообщения.")
		return
	}

	if text == "" {
		a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID, "⚠️ Не удалось распознать речь.")
		return
	}

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}

	// If user is in NL reminder creation state, route to the NL handler instead.
	if uc.State == tgstates.StateReminderCreateNL {
		tgBot.Request(tgbotapi.NewDeleteMessage(chatID, statusMsg.MessageID)) //nolint:errcheck
		a.handleReminderNLInput(ctx, tgBot, chatID, userID, text)
		return
	}

	if _, err := a.Core.AppendToNote(ctx, uc.ActiveDate, text); err != nil {
		log.Error("append to note", zap.Error(err))
		a.editStatus(ctx, tgBot, chatID, statusMsg.MessageID, "❌ Ошибка при сохранении в заметку.")
		return
	}

	kb := a.getMainMenuKeyboard(ctx)
	editText(ctx, tgBot, chatID, statusMsg.MessageID,
		fmt.Sprintf("🎙 Добавлено в заметку:\n\n%s", text), &kb)
}

func (a *App) editStatus(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
	editText(ctx, tgBot, chatID, msgID, text, nil)
}

func downloadFile(ctx context.Context, url string) (io.ReadCloser, error) {
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
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}
