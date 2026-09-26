package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"notes-bot/frontends/telegram/tgstates"
)

const FindResultsPerPage = 5

func FindPrompt() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("◀ Назад").WithCallbackData("menu:back"),
		),
	)
}

// FindResults builds an inline keyboard with one button per hit on the current
// page (label "📄 name"), pagination row, and a back-to-menu row.
func FindResults(hits []tgstates.SearchHit, page int) telego.InlineKeyboardMarkup {
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

	rows := make([][]telego.InlineKeyboardButton, 0, end-start+2)
	for i := start; i < end; i++ {
		h := hits[i]
		label := truncateRunes(fmt.Sprintf("📄 %s", h.Name), 56)
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("find:open:%d", h.NoteID)),
		))
	}

	if totalPages > 1 {
		nav := make([]telego.InlineKeyboardButton, 0, 3)
		if page > 0 {
			nav = append(nav, tu.InlineKeyboardButton("◀️").WithCallbackData(fmt.Sprintf("find:page:%d", page-1)))
		}
		nav = append(nav, tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", page+1, totalPages)).WithCallbackData("find:noop"))
		if page < totalPages-1 {
			nav = append(nav, tu.InlineKeyboardButton("▶️").WithCallbackData(fmt.Sprintf("find:page:%d", page+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🔁 Новый поиск").WithCallbackData("find:retry"),
		tu.InlineKeyboardButton("🏠 Меню").WithCallbackData("menu:back"),
	))

	return *tu.InlineKeyboard(rows...)
}

// NoteView builds the keyboard shown next to an opened note: append, back to
// results, back to main menu.
func NoteView(hasResults bool) telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✏️ Дописать").WithCallbackData("note:append"),
		),
	}
	if hasResults {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("↩️ К результатам").WithCallbackData("find:back"),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🏠 Меню").WithCallbackData("menu:back"),
	))
	return *tu.InlineKeyboard(rows...)
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
