package tghandlers

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// sendText sends a new text message to a chat with optional keyboard, using MarkdownV2.
func sendText(bot *tgbotapi.BotAPI, chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "MarkdownV2"
	msg.DisableNotification = true
	if keyboard != nil {
		msg.ReplyMarkup = *keyboard
	}
	_, err := bot.Send(msg)
	return err
}

// editText edits an existing message with optional keyboard, using MarkdownV2.
func editText(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "MarkdownV2"
	if keyboard != nil {
		edit.ReplyMarkup = keyboard
	}
	_, err := bot.Send(edit)
	return err
}

// replyToUpdate sends a reply to a message update.
func replyToUpdate(bot *tgbotapi.BotAPI, update *tgbotapi.Update, text string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	if update.Message == nil {
		return fmt.Errorf("update has no message")
	}
	return sendText(bot, update.Message.Chat.ID, text, keyboard)
}

// replyToCallback edits the message of a callback query.
func replyToCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, text string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	if query.Message == nil {
		return fmt.Errorf("callback has no message")
	}
	return editText(bot, query.Message.Chat.ID, query.Message.MessageID, text, keyboard)
}
