package tghandlers

import (
	"context"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/internal/telemetry"
)

// sendText sends a new text message to a chat with optional keyboard, using HTML parse mode.
func sendText(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, text tgfmt.HTML, keyboard *tgbotapi.InlineKeyboardMarkup, disableNotification bool) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	msg := tgbotapi.NewMessage(chatID, text.String())
	msg.ParseMode = "HTML"
	msg.DisableNotification = disableNotification
	if keyboard != nil {
		msg.ReplyMarkup = *keyboard
	}
	_, err := bot.Send(msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// editText edits an existing message with optional keyboard, using HTML parse mode.
func editText(ctx context.Context, bot *tgbotapi.BotAPI, chatID int64, messageID int, text tgfmt.HTML, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx, attribute.Int64("chat_id", chatID), attribute.Int("message_id", messageID))
	defer span.End()

	_, buildSpan := telemetry.StartSpan(ctx,
		attribute.Int("text_len", len(text)),
		attribute.Bool("has_keyboard", keyboard != nil),
	)
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text.String())
	edit.ParseMode = "HTML"
	if keyboard != nil {
		edit.ReplyMarkup = keyboard
	}
	buildSpan.End()

	_, sendSpan := telemetry.StartSpan(ctx)
	_, err := bot.Send(edit)
	if err != nil {
		sendSpan.RecordError(err)
		sendSpan.SetStatus(codes.Error, err.Error())
	}
	sendSpan.End()

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

// replyToUpdate sends a reply to a message update.
func replyToUpdate(ctx context.Context, bot *tgbotapi.BotAPI, update *tgbotapi.Update, text tgfmt.HTML, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if update.Message == nil {
		return fmt.Errorf("update has no message")
	}
	return sendText(ctx, bot, update.Message.Chat.ID, text, keyboard, true)
}

// replyToCallback edits the message of a callback query.
func replyToCallback(ctx context.Context, bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, text tgfmt.HTML, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if query.Message == nil {
		return fmt.Errorf("callback has no message")
	}
	return editText(ctx, bot, query.Message.Chat.ID, query.Message.MessageID, text, keyboard)
}
