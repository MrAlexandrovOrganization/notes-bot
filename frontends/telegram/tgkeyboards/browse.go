package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"notes-bot/frontends/telegram/clients"
)

const browsePageSize = 30

// BrowseNoteView keeps note actions within the vault browser.
func BrowseNoteView() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(tu.InlineKeyboardButton("✏️ Дописать").WithCallbackData("note:append")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton("🔙 Назад").WithCallbackData("browse:file_back")),
	)
}

func BrowseFolder(entries []clients.DirEntry, currentPath string, page int) telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{}

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
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(
				fmt.Sprintf("%s %s", icon, entry.Name)).WithCallbackData(
				fmt.Sprintf("browse:open:%d", idx),
			),
		))
	}

	navRow := []telego.InlineKeyboardButton{}
	if currentPath != "" {
		navRow = append(navRow, tu.InlineKeyboardButton("🔙 Назад").WithCallbackData("browse:up"))
	}
	navRow = append(navRow, tu.InlineKeyboardButton("🏠 Корень").WithCallbackData("browse:root"))

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	totalPages := (len(entries) + browsePageSize - 1) / browsePageSize
	if totalPages > 1 {
		paginationRow := []telego.InlineKeyboardButton{}
		if page > 0 {
			paginationRow = append(paginationRow,
				tu.InlineKeyboardButton("◀").WithCallbackData(fmt.Sprintf("browse:page:%d", page-1)),
			)
		}
		paginationRow = append(paginationRow,
			tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", page+1, totalPages)).WithCallbackData("browse:noop"),
		)
		if page < totalPages-1 {
			paginationRow = append(paginationRow,
				tu.InlineKeyboardButton("▶").WithCallbackData(fmt.Sprintf("browse:page:%d", page+1)),
			)
		}
		rows = append(rows, paginationRow)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀ В меню").WithCallbackData("menu:back"),
	))

	return *tu.InlineKeyboard(rows...)
}
