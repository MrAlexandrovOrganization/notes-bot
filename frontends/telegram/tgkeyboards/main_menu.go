package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

func RatingPrompt(hasRating bool, currentRating int) telego.InlineKeyboardMarkup {
	var label string
	if hasRating {
		label = fmt.Sprintf("Текущая оценка: %d", currentRating)
	} else {
		label = "Оценка не установлена"
	}
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData("menu:noop"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("← Назад").WithCallbackData("menu:back"),
		),
	)
}

func MainMenu(_ string) telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📊 Оценка").WithCallbackData("menu:rating"),
			tu.InlineKeyboardButton("✅ Задачи").WithCallbackData("menu:tasks"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📝 Заметка").WithCallbackData("menu:note"),
			tu.InlineKeyboardButton("📅 Календарь").WithCallbackData("menu:calendar"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔔 Уведомления").WithCallbackData("menu:notifications"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✨ Понять и сделать").WithCallbackData("menu:smart"),
			tu.InlineKeyboardButton("🔎 Найти заметку").WithCallbackData("menu:find"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🧠 Спросить").WithCallbackData("menu:ask"),
			tu.InlineKeyboardButton("📂 Обзор хранилища").WithCallbackData("menu:browse"),
		),
	)
}

// SmartConfirm — Да/Нет для подтверждения гипотезы LLM-классификатора.
func SmartConfirm() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✅ Да").WithCallbackData("smart:yes"),
			tu.InlineKeyboardButton("❌ Нет").WithCallbackData("smart:no"),
		),
	)
}

func SmartInput() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("◀ Назад").WithCallbackData("smart:back"),
		),
	)
}

// SmartIntentPicker показывается, когда LLM не уверена или не поняла —
// пользователь выбирает intent вручную.
func SmartIntentPicker() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📝 Заметка").WithCallbackData("smart:pick:note"),
			tu.InlineKeyboardButton("✅ Задача").WithCallbackData("smart:pick:task"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("⏰ Напоминание").WithCallbackData("smart:pick:reminder"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("smart:no"),
		),
	)
}
