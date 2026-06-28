package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"notes-bot/frontends/telegram/tgstates"
)

const FindResultsPerPage = 5

// FindResults builds an inline keyboard with one button per hit on the current
// page (label "📄 name"), pagination row, and a back-to-menu row.
func FindResults(hits []tgstates.SearchHit, page int) tgbotapi.InlineKeyboardMarkup {
	if page < 0 {
		page = 0
	}
	totalPages := (len(hits) + FindResultsPerPage - 1) / FindResultsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	start := page * FindResultsPerPage
	end := min(start+FindResultsPerPage, len(hits))

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, end-start+2)
	for i := start; i < end; i++ {
		h := hits[i]
		label := truncateRunes(fmt.Sprintf("📄 %s", h.Name), 56)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("find:open:%d", h.NoteID)),
		))
	}

	if totalPages > 1 {
		nav := make([]tgbotapi.InlineKeyboardButton, 0, 3)
		if page > 0 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀️", fmt.Sprintf("find:page:%d", page-1)))
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "find:noop"))
		if page < totalPages-1 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("▶️", fmt.Sprintf("find:page:%d", page+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔁 Новый поиск", "find:retry"),
		tgbotapi.NewInlineKeyboardButtonData("🏠 Меню", "menu:back"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// NoteView builds the keyboard shown next to an opened note: append, back to
// results, back to main menu.
func NoteView(hasResults bool) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Дописать", "note:append"),
		),
	}
	if hasResults {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ К результатам", "find:back"),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 Меню", "menu:back"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// truncateRunes returns s truncated to maxRunes runes, with a trailing ellipsis if cut.
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-1]) + "…"
}
