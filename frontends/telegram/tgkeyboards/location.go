package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func LocationTrackingMenu(isActive bool) tgbotapi.InlineKeyboardMarkup {
	status := "❌ Выключено"
	if isActive {
		status = "✅ Включено"
	}
	var toggleLabel string
	if isActive {
		toggleLabel = "🔇 Выключить геолокацию"
	} else {
		toggleLabel = "📍 Включить геолокацию"
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Статус: "+status, "location:noop"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(toggleLabel, "location:toggle"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 История", "location:history"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("← Назад в меню", "menu:back"),
		),
	)
}

func LocationHistoryPage(page int, hasMore bool) tgbotapi.InlineKeyboardMarkup {
	rows := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("← Назад", "location:menu"),
	}
	if hasMore {
		rows = append(rows, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("Ещё ▶"), fmt.Sprintf("location:page:%d", page+1)),
		)
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(rows...),
	)
}
