package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func RatingPrompt(hasRating bool, currentRating int) tgbotapi.InlineKeyboardMarkup {
	var label string
	if hasRating {
		label = fmt.Sprintf("Текущая оценка: %d", currentRating)
	} else {
		label = "Оценка не установлена"
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "menu:noop"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("← Назад", "menu:back"),
		),
	)
}

func MainMenu(_ string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Оценка", "menu:rating"),
			tgbotapi.NewInlineKeyboardButtonData("✅ Задачи", "menu:tasks"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 Заметка", "menu:note"),
			tgbotapi.NewInlineKeyboardButtonData("📅 Календарь", "menu:calendar"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔔 Уведомления", "menu:notifications"),
		),
	)
}
