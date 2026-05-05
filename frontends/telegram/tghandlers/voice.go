package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	pb "notes-bot/proto/whisper"
)

const (
	voicePollInterval = 5 * time.Second
	voicePollDeadline = 3 * time.Hour
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

	// Reply to the voice message with status — this message will be updated with progress.
	replyMsg := tgbotapi.NewMessage(chatID, "⏳ Скачиваю аудио...")
	replyMsg.ReplyToMessageID = update.Message.MessageID
	statusMsg, err := tgBot.Send(replyMsg)
	if err != nil {
		log.Error("send status message", zap.Error(err))
		return
	}

	// Send a separate message with menu so the user can continue interacting.
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, "Голосовое принято, можешь продолжать работу.", &kb, true)

	// Get user state now (before goroutine) to check NL reminder mode.
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}

	// Run the rest asynchronously so the bot stays responsive.
	go a.processVoice(tgBot, chatID, userID, statusMsg.MessageID, fileID, format, uc, log)
}

// processVoice handles file download, whisper submission, polling, and result delivery in background.
func (a *App) processVoice(tgBot *tgbotapi.BotAPI, chatID, userID int64, statusMsgID int, fileID, format string, uc *tgstates.UserContext, log *zap.Logger) {
	ctx := context.Background()

	// Download the file.
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := tgBot.GetFile(fileConfig)
	if err != nil {
		log.Error("get file", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при загрузке файла.")
		return
	}

	fileURL := tgFile.Link(tgBot.Token)
	rc, err := downloadFile(ctx, fileURL)
	if err != nil {
		log.Error("download file", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при загрузке файла.")
		return
	}
	defer rc.Close()

	// Submit to whisper.
	editStatus(ctx, tgBot, chatID, statusMsgID, "⏳ Отправляю на расшифровку...")

	jobID, queuePos, err := a.Whisper.Submit(ctx, rc, format, "voice")
	if err != nil {
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			editStatus(ctx, tgBot, chatID, statusMsgID,
				"⏳ Сервис распознавания ещё запускается. Попробуйте через несколько секунд.")
			return
		}
		log.Error("submit error", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при обработке голосового сообщения.")
		return
	}
	log.Info("whisper job submitted", zap.String("job_id", jobID), zap.Int("queue_pos", queuePos))

	// Show initial status with cancel button.
	statusText := "⏳ Расшифровываю..."
	if queuePos > 1 {
		statusText = fmt.Sprintf("⏳ В очереди (позиция %d), подожди немного...", queuePos)
	}
	cancelKb := voiceCancelKeyboard(jobID)
	editText(ctx, tgBot, chatID, statusMsgID, statusText, &cancelKb)

	// Set up cancellation.
	pollCtx, pollCancel := context.WithCancel(ctx)
	a.voiceCancels.Store(jobID, pollCancel)

	// Poll for result with progress updates.
	text, err := a.pollTranscription(pollCtx, tgBot, chatID, statusMsgID, jobID, log)
	a.voiceCancels.Delete(jobID)
	pollCancel()

	if err != nil {
		if pollCtx.Err() != nil {
			editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Отменено.")
			return
		}
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			editStatus(ctx, tgBot, chatID, statusMsgID,
				"⏳ Сервис распознавания недоступен. Попробуйте позже.")
			return
		}
		log.Error("transcription error", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при обработке голосового сообщения.")
		return
	}

	if text == "" {
		editStatus(ctx, tgBot, chatID, statusMsgID, "⚠️ Не удалось распознать речь.")
		return
	}

	// Re-read user state — it may have changed while we were transcribing.
	uc, err = a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}

	// If user is in NL reminder creation state, route to the NL handler.
	if uc.State == tgstates.StateReminderCreateNL {
		tgBot.Request(tgbotapi.NewDeleteMessage(chatID, statusMsgID)) //nolint:errcheck
		a.handleReminderNLInput(ctx, tgBot, chatID, userID, text)
		return
	}

	if _, err := a.Core.AppendToNote(ctx, uc.ActiveDate, text); err != nil {
		log.Error("append to note", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при сохранении в заметку.")
		return
	}

	editText(ctx, tgBot, chatID, statusMsgID,
		fmt.Sprintf("🎙 Добавлено в заметку:\n\n%s", text), nil)
}

// pollTranscription polls whisper service for job completion, updating the status message with progress.
func (a *App) pollTranscription(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, jobID string, log *zap.Logger) (string, error) {
	ticker := time.NewTicker(voicePollInterval)
	defer ticker.Stop()
	deadline := time.After(voicePollDeadline)

	cancelKb := voiceCancelKeyboard(jobID)
	lastStatusText := ""

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", fmt.Errorf("poll deadline exceeded (3h)")
		case <-ticker.C:
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			result, err := a.Whisper.GetStatus(ctx, jobID)
			if err != nil {
				log.Warn("poll status error", zap.String("job_id", jobID), zap.Error(err))
				continue
			}

			switch result.Status {
			case pb.JobStatus_ACCEPTED, pb.JobStatus_DOWNLOADING, pb.JobStatus_QUEUED:
				statusText := "⏳ В очереди..."
				if statusText != lastStatusText {
					editText(context.Background(), tgBot, chatID, msgID, statusText, &cancelKb)
					lastStatusText = statusText
				}
			case pb.JobStatus_RUNNING:
				statusText := "⏳ Расшифровываю..."
				if result.ProgressPercent > 0 {
					statusText = fmt.Sprintf("⏳ Расшифровываю... %.0f%%", result.ProgressPercent)
				}
				if statusText != lastStatusText {
					editText(context.Background(), tgBot, chatID, msgID, statusText, &cancelKb)
					lastStatusText = statusText
				}
			case pb.JobStatus_DONE:
				return result.Text, nil
			case pb.JobStatus_FAILED:
				if result.Error == "cancelled" {
					return "", ctx.Err()
				}
				return "", fmt.Errorf("transcription failed: %s", result.Error)
			}
		}
	}
}

func voiceCancelKeyboard(jobID string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("voice:cancel:%s", jobID)),
		),
	)
}

// handleVoiceAction handles voice-related callback actions (cancel).
func (a *App) handleVoiceAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 3 || parts[1] != "cancel" {
		return nil
	}
	jobID := strings.Join(parts[2:], ":")
	a.cancelVoiceJob(ctx, jobID)
	return nil
}

func editStatus(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, text string) {
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
