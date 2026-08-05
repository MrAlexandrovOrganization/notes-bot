package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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

func noteNavigation(currentPage, totalPages int, callbackPrefix, noopCallback string) []tgbotapi.InlineKeyboardButton {
	if totalPages <= 1 {
		return nil
	}

	nav := make([]tgbotapi.InlineKeyboardButton, 0, 3)
	if currentPage > 0 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀ Назад", fmt.Sprintf("%s:%d", callbackPrefix, currentPage-1)))
	}
	nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", currentPage+1, totalPages), noopCallback))
	if currentPage < totalPages-1 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("Далее ▶", fmt.Sprintf("%s:%d", callbackPrefix, currentPage+1)))
	}
	return nav
}

// NotePagination создает клавиатуру с пагинацией для заметки.
// Возвращает текст заметки (разбитый на страницы) и клавиатуру с навигацией.
func NotePagination(content string, currentPage int) (string, *tgbotapi.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]tgbotapi.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "note:page", "note:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀ В меню", "note:back"),
	))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return pageContent, &keyboard
}

// BrowseFilePagination paginates a note opened through the vault browser.
func BrowseFilePagination(content string, currentPage int) (string, *tgbotapi.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]tgbotapi.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "browse:file_page", "browse:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "browse:file_back"),
	))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return pageContent, &keyboard
}

// FoundNotePagination paginates a note opened from search results.
func FoundNotePagination(content string, currentPage int, hasResults bool) (string, *tgbotapi.InlineKeyboardMarkup) {
	pageContent, currentPage, totalPages := notePage(content, currentPage)
	var rows [][]tgbotapi.InlineKeyboardButton
	if nav := noteNavigation(currentPage, totalPages, "find:note_page", "find:noop"); nav != nil {
		rows = append(rows, nav)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✏️ Дописать", "note:append"),
	))
	if hasResults {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ К результатам", "find:back"),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🏠 Меню", "menu:back"),
	))
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return pageContent, &keyboard
}
