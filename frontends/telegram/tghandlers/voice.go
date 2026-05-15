package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
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
	// voiceCharsPerPage is the max text length per page.
	// We reserve space for the header, code block markers, and page indicator.
	voiceCharsPerPage = 3200
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

	// Fix the active date at the moment the voice message is sent,
	// so switching days while transcription is in progress won't affect it.
	activeDate := uc.ActiveDate
	isNLReminder := uc.State == tgstates.StateReminderCreateNL

	// Run the rest asynchronously so the bot stays responsive.
	go a.processVoice(tgBot, chatID, userID, statusMsg.MessageID, fileID, format, activeDate, isNLReminder, log)
}

// processVoice handles file download, whisper submission, polling, and result delivery in background.
func (a *App) processVoice(tgBot *tgbotapi.BotAPI, chatID, userID int64, statusMsgID int, fileID, format, activeDate string, isNLReminder bool, log *zap.Logger) {
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in processVoice", zap.Any("recover", r), zap.String("stack", string(debug.Stack())))
			editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Произошла внутренняя ошибка.")
		}
	}()

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
		if _, ok := errors.AsType[*clients.ServiceUnavailableError](err); ok {
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
	// Pass statusText so the poller doesn't immediately overwrite the queue position message.
	text, err := a.pollTranscription(pollCtx, tgBot, chatID, statusMsgID, jobID, statusText, log)
	a.voiceCancels.Delete(jobID)
	pollCancel()

	if err != nil {
		if pollCtx.Err() != nil {
			editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Отменено.")
			return
		}
		if _, ok := errors.AsType[*clients.ServiceUnavailableError](err); ok {
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

	// If user was in NL reminder creation state when they sent the voice, route to the NL handler.
	if isNLReminder {
		tgBot.Request(tgbotapi.NewDeleteMessage(chatID, statusMsgID)) //nolint:errcheck
		a.handleReminderNLInput(ctx, tgBot, chatID, userID, text)
		return
	}

	// Use the date that was active when the voice message was sent.
	if _, err := a.Core.AppendToNote(ctx, activeDate, text); err != nil {
		log.Error("append to note", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при сохранении в заметку.")
		return
	}

	// Store the text for pagination and show the first page.
	a.voiceTexts.Store(statusMsgID, text)
	if err := a.showVoicePage(ctx, tgBot, chatID, statusMsgID, text, 0); err != nil {
		log.Error("show voice page failed, sending as plain message", zap.Error(err))
		// Fallback: send the transcription as a new plain message so the user gets the result.
		runes := []rune(text)
		preview := string(runes[:min(len(runes), 3500)])
		suffix := ""
		if len(runes) > 3500 {
			suffix = "\n\n_(используй кнопки навигации или попробуй снова)_"
		}
		sendText(ctx, tgBot, chatID, "🎙 Расшифровка:\n\n"+preview+suffix, nil, true)
	}
}

// showVoicePage renders a specific page of the transcription result.
func (a *App) showVoicePage(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, fullText string, page int) error {
	runes := []rune(fullText)
	totalPages := (len(runes) + voiceCharsPerPage - 1) / voiceCharsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * voiceCharsPerPage
	end := min(start+voiceCharsPerPage, len(runes))
	pageText := string(runes[start:end])

	// Build the message with code block.
	// The header is escaped for MarkdownV2, but the code block content
	// only needs ` and \ escaped (MarkdownV2 code block rules).
	var header string
	if totalPages > 1 {
		header = EscapeMarkdownV2(fmt.Sprintf("🎙 Добавлено в заметку [%d/%d]:", page+1, totalPages))
	} else {
		header = EscapeMarkdownV2("🎙 Добавлено в заметку:")
	}
	codeContent := escapeCodeBlock(pageText)
	msg := fmt.Sprintf("%s\n\n```\n%s\n```", header, codeContent)

	var kb *tgbotapi.InlineKeyboardMarkup
	if totalPages > 1 {
		keyboard := voicePaginationKeyboard(msgID, page, totalPages)
		kb = &keyboard
	}

	return editTextRaw(ctx, tgBot, chatID, msgID, msg, kb)
}

// escapeCodeBlock escapes only the characters that need escaping inside
// a MarkdownV2 code block: ` and \.
func escapeCodeBlock(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

func voicePaginationKeyboard(msgID, currentPage, totalPages int) tgbotapi.InlineKeyboardMarkup {
	var nav []tgbotapi.InlineKeyboardButton
	if currentPage > 0 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀ Назад",
			fmt.Sprintf("voice:page:%d:%d", msgID, currentPage-1)))
	}
	nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(
		fmt.Sprintf("%d/%d", currentPage+1, totalPages), "voice:noop"))
	if currentPage < totalPages-1 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("Далее ▶",
			fmt.Sprintf("voice:page:%d:%d", msgID, currentPage+1)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(nav)
}

// pollTranscription polls whisper service for job completion, updating the status message with progress.
// initialStatusText is the text already shown before polling started — used to avoid an
// unnecessary duplicate edit on the first tick.
func (a *App) pollTranscription(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, jobID string, initialStatusText string, log *zap.Logger) (string, error) {
	ticker := time.NewTicker(voicePollInterval)
	defer ticker.Stop()
	deadline := time.After(voicePollDeadline)

	cancelKb := voiceCancelKeyboard(jobID)
	lastStatusText := initialStatusText
	consecutiveErrors := 0

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
				consecutiveErrors++
				log.Warn("poll status error", zap.String("job_id", jobID), zap.Int("consecutive_errors", consecutiveErrors), zap.Error(err))
				if consecutiveErrors >= 10 {
					return "", fmt.Errorf("status polling failed %d times in a row: %w", consecutiveErrors, err)
				}
				continue
			}
			consecutiveErrors = 0

			switch result.Status {
			case pb.JobStatus_ACCEPTED, pb.JobStatus_QUEUED:
				statusText := "⏳ В очереди..."
				if statusText != lastStatusText {
					editText(context.Background(), tgBot, chatID, msgID, statusText, &cancelKb)
					lastStatusText = statusText
				}
			case pb.JobStatus_DOWNLOADING:
				statusText := "⏳ Загружаю аудио в сервис..."
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

// handleVoiceAction handles voice-related callback actions (cancel, page, noop).
func (a *App) handleVoiceAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}

	switch parts[1] {
	case "cancel":
		if len(parts) < 3 {
			return nil
		}
		jobID := strings.Join(parts[2:], ":")
		a.cancelVoiceJob(ctx, jobID)

	case "page":
		// voice:page:<msgID>:<page>
		if len(parts) < 4 {
			return nil
		}
		msgID, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
		page, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil
		}
		val, ok := a.voiceTexts.Load(msgID)
		if !ok {
			return nil
		}
		chatID := query.Message.Chat.ID
		//nolint:errcheck — pagination errors on callback are non-critical
		a.showVoicePage(ctx, tgBot, chatID, msgID, val.(string), page)

	case "noop":
		// Do nothing — just the page counter button.
	}
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
