package tgkeyboards

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

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
