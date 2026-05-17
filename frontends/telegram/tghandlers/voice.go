package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// pendingVoiceResult holds everything needed to deliver one transcription.
// It is created and registered in the reorder buffer before the goroutine
// that fills it starts, so that the ordering slot exists immediately.
type pendingVoiceResult struct {
	// Input — set at registration time.
	tgBot        *tgbotapi.BotAPI
	chatID       int64
	userID       int64
	statusMsgID  int
	activeDate   string
	isNLReminder bool
	log          *zap.Logger

	// Output — set by transcribeVoice when it finishes.
	done         bool
	text         string
	transcribeErr error
}

// voiceReorderBuffer ensures transcription results are delivered in Telegram
// MessageID order (= user send order) regardless of which file finished first.
//
// How it works:
//   - register() is called in HandleVoiceMessage (before the goroutine) to
//     claim a slot for the message.
//   - complete() is called by the goroutine when transcription finishes.
//     It returns all consecutive completed results from the front of the
//     queue — those are safe to deliver in order right now.
//   - Flushing only proceeds as far as the first still-pending slot, so a
//     fast small message simply waits until all earlier messages are done.
type voiceReorderBuffer struct {
	mu      sync.Mutex
	ids     []int                       // MessageIDs sorted ascending
	results map[int]*pendingVoiceResult // nil = impossible after register
}

func newVoiceReorderBuffer() *voiceReorderBuffer {
	return &voiceReorderBuffer{results: make(map[int]*pendingVoiceResult)}
}

func (b *voiceReorderBuffer) register(msgID int, r *pendingVoiceResult) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := sort.SearchInts(b.ids, msgID)
	b.ids = append(b.ids, 0)
	copy(b.ids[i+1:], b.ids[i:])
	b.ids[i] = msgID
	b.results[msgID] = r
}

// complete marks msgID as done and returns all consecutive completed results
// from the front of the queue — these must be delivered in order by the caller.
func (b *voiceReorderBuffer) complete(msgID int, text string, transcribeErr error) []*pendingVoiceResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r, ok := b.results[msgID]; ok {
		r.text = text
		r.transcribeErr = transcribeErr
		r.done = true
	}
	var ready []*pendingVoiceResult
	for len(b.ids) > 0 {
		r := b.results[b.ids[0]]
		if r == nil || !r.done {
			break
		}
		delete(b.results, b.ids[0])
		b.ids = b.ids[1:]
		ready = append(ready, r)
	}
	return ready
}

// getVoiceBuffer returns (or lazily creates) the per-user reorder buffer.
func (a *App) getVoiceBuffer(userID int64) *voiceReorderBuffer {
	buf := newVoiceReorderBuffer()
	actual, _ := a.voiceBuffers.LoadOrStore(userID, buf)
	return actual.(*voiceReorderBuffer)
}

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

	// Get user state now (before goroutine) to capture NL reminder mode and active date.
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		return
	}
	activeDate := uc.ActiveDate
	isNLReminder := uc.State == tgstates.StateReminderCreateNL

	// Reply with initial status — the goroutine will update it with progress.
	replyMsg := tgbotapi.NewMessage(chatID, "⏳ Принято...")
	replyMsg.ReplyToMessageID = update.Message.MessageID
	statusMsg, err := tgBot.Send(replyMsg)
	if err != nil {
		log.Error("send status message", zap.Error(err))
		return
	}

	// Send the main-menu message so the user can keep working.
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, "Голосовое принято, можешь продолжать работу.", &kb, true)

	// Register in the reorder buffer BEFORE launching the goroutine.
	// This claims the slot so that complete() for an earlier message can
	// see this one as still-pending and correctly hold the flush.
	result := &pendingVoiceResult{
		tgBot:        tgBot,
		chatID:       chatID,
		userID:       userID,
		statusMsgID:  statusMsg.MessageID,
		activeDate:   activeDate,
		isNLReminder: isNLReminder,
		log:          log,
	}
	buf := a.getVoiceBuffer(userID)
	msgID := update.Message.MessageID
	buf.register(msgID, result)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic in voice goroutine",
					zap.Any("recover", r),
					zap.String("stack", string(debug.Stack())))
				editStatus(context.Background(), tgBot, chatID, statusMsg.MessageID,
					"❌ Произошла внутренняя ошибка.")
				// Always unblock the buffer so later messages can still be delivered.
				for _, rr := range buf.complete(msgID, "", fmt.Errorf("panic")) {
					a.deliverVoiceResult(rr)
				}
			}
		}()

		text, transcribeErr := a.transcribeVoice(tgBot, chatID, statusMsg.MessageID, fileID, format, log)

		ready := buf.complete(msgID, text, transcribeErr)

		// If this message's transcription succeeded but it is being held
		// (waiting for an earlier message to finish), show an indicator so
		// the user knows the result is ready but kept in order.
		if transcribeErr == nil && text != "" && len(ready) == 0 {
			editStatus(context.Background(), tgBot, chatID, statusMsg.MessageID,
				"⏳ Расшифровано, жду очерёдности...")
		}

		for _, rr := range ready {
			a.deliverVoiceResult(rr)
		}
	}()
}

// transcribeVoice downloads the audio file, submits it to Whisper, and polls
// until transcription completes or fails. It keeps the Telegram status message
// updated throughout. Returns the recognised text (empty if nothing was heard)
// and any fatal error. All user-visible error messages are sent here.
func (a *App) transcribeVoice(tgBot *tgbotapi.BotAPI, chatID int64, statusMsgID int, fileID, format string, log *zap.Logger) (string, error) {
	ctx := context.Background()

	// Download the file.
	editStatus(ctx, tgBot, chatID, statusMsgID, "⏳ Скачиваю аудио...")
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	tgFile, err := tgBot.GetFile(fileConfig)
	if err != nil {
		log.Error("get file", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при загрузке файла.")
		return "", err
	}

	fileURL := tgFile.Link(tgBot.Token)
	rc, err := downloadFile(ctx, fileURL)
	if err != nil {
		log.Error("download file", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при загрузке файла.")
		return "", err
	}
	defer rc.Close()

	// Submit to Whisper.
	editStatus(ctx, tgBot, chatID, statusMsgID, "⏳ Отправляю на расшифровку...")
	jobID, queuePos, err := a.Whisper.Submit(ctx, rc, format, "voice")
	if err != nil {
		if _, ok := errors.AsType[*clients.ServiceUnavailableError](err); ok {
			editStatus(ctx, tgBot, chatID, statusMsgID,
				"⏳ Сервис распознавания ещё запускается. Попробуйте через несколько секунд.")
			return "", err
		}
		log.Error("submit error", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при обработке голосового сообщения.")
		return "", err
	}
	log.Info("whisper job submitted", zap.String("job_id", jobID), zap.Int("queue_pos", queuePos))

	// Show initial polling status with cancel button.
	statusText := "⏳ Расшифровываю..."
	if queuePos > 1 {
		statusText = fmt.Sprintf("⏳ В очереди (позиция %d), подожди немного...", queuePos)
	}
	cancelKb := voiceCancelKeyboard(jobID)
	editText(ctx, tgBot, chatID, statusMsgID, statusText, &cancelKb)

	// Set up cancellation.
	pollCtx, pollCancel := context.WithCancel(ctx)
	a.voiceCancels.Store(jobID, pollCancel)
	defer func() {
		a.voiceCancels.Delete(jobID)
		pollCancel()
	}()

	// Poll for result.
	text, err := a.pollTranscription(pollCtx, tgBot, chatID, statusMsgID, jobID, statusText, queuePos, log)
	if err != nil {
		if pollCtx.Err() != nil {
			editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Отменено.")
			return "", err
		}
		if _, ok := errors.AsType[*clients.ServiceUnavailableError](err); ok {
			editStatus(ctx, tgBot, chatID, statusMsgID,
				"⏳ Сервис распознавания недоступен. Попробуйте позже.")
			return "", err
		}
		log.Error("transcription error", zap.Error(err))
		editStatus(ctx, tgBot, chatID, statusMsgID, "❌ Ошибка при обработке голосового сообщения.")
		return "", err
	}

	if text == "" {
		editStatus(ctx, tgBot, chatID, statusMsgID, "⚠️ Не удалось распознать речь.")
		return "", nil
	}

	return text, nil
}

// deliverVoiceResult appends text to the note and updates the status message.
// It is only called once a result has reached the front of the reorder queue,
// so note appends always happen in the user's original send order.
func (a *App) deliverVoiceResult(r *pendingVoiceResult) {
	// Errors and empty transcriptions were already surfaced in transcribeVoice.
	if r.transcribeErr != nil || r.text == "" {
		return
	}

	ctx := context.Background()

	// If user was in NL reminder creation state, route to the NL handler.
	if r.isNLReminder {
		r.tgBot.Request(tgbotapi.NewDeleteMessage(r.chatID, r.statusMsgID)) //nolint:errcheck
		a.handleReminderNLInput(ctx, r.tgBot, r.chatID, r.userID, r.text)
		return
	}

	// Append to the note that was active when the message was sent.
	if _, err := a.Core.AppendToNote(ctx, r.activeDate, r.text); err != nil {
		r.log.Error("append to note", zap.Error(err))
		editStatus(ctx, r.tgBot, r.chatID, r.statusMsgID, "❌ Ошибка при сохранении в заметку.")
		return
	}

	// Store text for pagination and show the first page.
	a.voiceTexts.Store(r.statusMsgID, r.text)
	if err := a.showVoicePage(ctx, r.tgBot, r.chatID, r.statusMsgID, r.text, 0); err != nil {
		r.log.Error("show voice page failed, sending as plain message", zap.Error(err))
		runes := []rune(r.text)
		preview := string(runes[:min(len(runes), 3500)])
		suffix := ""
		if len(runes) > 3500 {
			suffix = "\n\n_(используй кнопки навигации или попробуй снова)_"
		}
		sendText(ctx, r.tgBot, r.chatID, "🎙 Расшифровка:\n\n"+preview+suffix, nil, true)
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
// whisperQueuePos is the position returned by Submit (1 = running, 2+ = waiting); kept visible
// while the job is queued since StatusResponse does not include a live position field.
func (a *App) pollTranscription(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, msgID int, jobID string, initialStatusText string, whisperQueuePos int, log *zap.Logger) (string, error) {
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
				// Keep the queue position from Submit visible — StatusResponse does
				// not carry a live position field, so we reuse the value from Submit.
				statusText := "⏳ В очереди..."
				if whisperQueuePos > 1 {
					statusText = fmt.Sprintf("⏳ В очереди (позиция %d), подожди немного...", whisperQueuePos)
				}
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
