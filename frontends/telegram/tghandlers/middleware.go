package tghandlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/internal/telemetry"
)

// isRetriableNetworkError reports whether err is a transient TCP error safe to retry
// (stale keep-alive connection reset by the remote side before our request was processed).
func isRetriableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "use of closed network connection")
}

// sendText sends a new text message to a chat with optional keyboard, using HTML parse mode.
func sendText(ctx context.Context, bot *telego.Bot, chatID int64, text tgfmt.HTML, keyboard *telego.InlineKeyboardMarkup, disableNotification bool) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	msg := &telego.SendMessageParams{ChatID: telego.ChatID{ID: chatID}, Text: text.String()}
	msg.ParseMode = "HTML"
	msg.DisableNotification = disableNotification
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	var err error
	for attempt := range 2 {
		_, err = bot.SendMessage(ctx, msg)
		if err == nil || !isRetriableNetworkError(err) || attempt == 1 {
			break
		}
		if waitErr := waitTelegramRetry(ctx); waitErr != nil {
			err = waitErr
			break
		}
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// editText edits an existing message with optional keyboard, using HTML parse mode.
func editText(ctx context.Context, bot *telego.Bot, chatID int64, messageID int, text tgfmt.HTML, keyboard *telego.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx, attribute.Int64("chat_id", chatID), attribute.Int("message_id", messageID))
	defer span.End()

	edit := &telego.EditMessageTextParams{ChatID: telego.ChatID{ID: chatID}, MessageID: messageID, Text: text.String()}
	edit.ParseMode = "HTML"
	if keyboard != nil {
		edit.ReplyMarkup = keyboard
	}
	var err error
	for attempt := range 2 {
		sendCtx, sendSpan := telemetry.StartSpan(ctx)
		_, err = bot.EditMessageText(sendCtx, edit)
		if err != nil {
			sendSpan.RecordError(err)
			sendSpan.SetStatus(codes.Error, err.Error())
		}
		sendSpan.End()
		if err == nil || !isRetriableNetworkError(err) || attempt == 1 {
			break
		}
		if waitErr := waitTelegramRetry(ctx); waitErr != nil {
			err = waitErr
			break
		}
	}

	if err != nil {
		if strings.Contains(err.Error(), "message is not modified") {
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Int("text_len", len(text)))
	}
	return err
}

func waitTelegramRetry(ctx context.Context) error {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// replyToUpdate sends a reply to a message update.
func replyToUpdate(ctx context.Context, bot *telego.Bot, update *telego.Update, text tgfmt.HTML, keyboard *telego.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if update.Message == nil {
		return fmt.Errorf("update has no message")
	}
	return sendText(ctx, bot, update.Message.Chat.ID, text, keyboard, true)
}

// replyToCallback edits the message of a callback query.
func replyToCallback(ctx context.Context, bot *telego.Bot, query *telego.CallbackQuery, text tgfmt.HTML, keyboard *telego.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if query.Message == nil {
		return fmt.Errorf("callback has no message")
	}
	return editText(ctx, bot, query.Message.GetChat().ID, query.Message.GetMessageID(), text, keyboard)
}
