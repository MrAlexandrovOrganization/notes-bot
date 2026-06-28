package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"notes-bot/frontends/telegram/clients"
)

const browsePageSize = 30

func BrowseFolder(entries []clients.DirEntry, currentPath string, page int) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}

	start := page * browsePageSize
	end := start + browsePageSize
	if end > len(entries) {
		end = len(entries)
	}
	pageEntries := entries[start:end]

	for i, entry := range pageEntries {
		icon := "📄"
		if entry.IsDir {
			icon = "📁"
		}
		idx := start + i
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%s %s", icon, entry.Name),
				fmt.Sprintf("browse:open:%d", idx),
			),
		))
	}

	navRow := []tgbotapi.InlineKeyboardButton{}
	if currentPath != "" {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "browse:up"))
	}
	navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("🏠 Корень", "browse:root"))

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	totalPages := (len(entries) + browsePageSize - 1) / browsePageSize
	if totalPages > 1 {
		paginationRow := []tgbotapi.InlineKeyboardButton{}
		if page > 0 {
			paginationRow = append(paginationRow,
				tgbotapi.NewInlineKeyboardButtonData("◀", fmt.Sprintf("browse:page:%d", page-1)),
			)
		}
		paginationRow = append(paginationRow,
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "browse:noop"),
		)
		if page < totalPages-1 {
			paginationRow = append(paginationRow,
				tgbotapi.NewInlineKeyboardButtonData("▶", fmt.Sprintf("browse:page:%d", page+1)),
			)
		}
		rows = append(rows, paginationRow)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀ В меню", "menu:back"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
