package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const noteCharsPerPage = 3500

func notePage(content string, currentPage int) (string, int, int) {
	runes := []rune(content)
	totalChars := len(runes)
	totalPages := (totalChars + noteCharsPerPage - 1) / noteCharsPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	currentPage = max(0, min(currentPage, totalPages-1))
	startIdx := currentPage * noteCharsPerPage
	endIdx := min(startIdx+noteCharsPerPage, totalChars)
	pageContent := string(runes[startIdx:endIdx])

	if totalPages > 1 {
		pageContent = fmt.Sprintf("[Страница %d/%d]\n\n%s", currentPage+1, totalPages, pageContent)
	}
	return pageContent, currentPage, totalPages
}

func noteNavigation(currentPage, totalPages int, callbackPrefix, noopCallback string) []telego.InlineKeyboardButton {
	if totalPages <= 1 {
		return nil
	}

	nav := make([]telego.InlineKeyboardButton, 0, 3)
	if currentPage > 0 {
		nav = append(nav, tu.InlineKeyboardButton("◀ Назад").WithCallbackData(fmt.Sprintf("%s:%d", callbackPrefix, currentPage-1)))
	}
	nav = append(nav, tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", currentPage+1, totalPages)).WithCallbackData(noopCallback))
	if currentPage < totalPages-1 {
		nav = append(nav, tu.InlineKeyboardButton("Далее ▶").WithCallbackData(fmt.Sprintf("%s:%d", callbackPrefix, currentPage+1)))
	}
	return nav
}

// NotePagination создает клавиатуру с пагинацией для заметки.
// Возвращает текст заметки (разбитый на страницы) и клавиатуру с навигацией.
func NotePagination(content string, currentPage int) (string, *telego.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]telego.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "note:page", "note:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀ В меню").WithCallbackData("note:back"),
	))
	return pageContent, tu.InlineKeyboard(rows...)
}

// BrowseFilePagination paginates a note opened through the vault browser.
func BrowseFilePagination(content string, currentPage int) (string, *telego.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]telego.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "browse:file_page", "browse:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, BrowseNoteView().InlineKeyboard...)
	return pageContent, tu.InlineKeyboard(rows...)
}

// FoundNotePagination paginates a note opened from search results.
func FoundNotePagination(content string, currentPage int, hasResults bool) (string, *telego.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]telego.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "find:note_page", "find:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("✏️ Дописать").WithCallbackData("note:append"),
	))
	if hasResults {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("↩️ К результатам").WithCallbackData("find:back"),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🏠 Меню").WithCallbackData("menu:back"),
	))
	return pageContent, tu.InlineKeyboard(rows...)
}
