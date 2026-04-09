package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const noteCharsPerPage = 3500

// NotePagination создает клавиатуру с пагинацией для заметки.
// Возвращает текст заметки (разбитый на страницы) и клавиатуру с навигацией.
func NotePagination(content string, currentPage int) (string, *tgbotapi.InlineKeyboardMarkup) {
	// Подсчитываем общее количество страниц
	totalChars := len(content)
	totalPages := (totalChars + noteCharsPerPage - 1) / noteCharsPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	// Ограничиваем текущую страницу
	if currentPage < 0 {
		currentPage = 0
	}
	if currentPage >= totalPages {
		currentPage = totalPages - 1
	}

	// Получаем текст для текущей страницы
	startIdx := currentPage * noteCharsPerPage
	endIdx := startIdx + noteCharsPerPage
	if endIdx > totalChars {
		endIdx = totalChars
	}

	pageContent := content[startIdx:endIdx]

	// Добавляем индикатор страницы, если их несколько
	if totalPages > 1 {
		pageContent = fmt.Sprintf("[Страница %d/%d]\n\n%s", currentPage+1, totalPages, pageContent)
	}

	// Создаем клавиатуру
	var rows [][]tgbotapi.InlineKeyboardButton

	if totalPages > 1 {
		var nav []tgbotapi.InlineKeyboardButton
		if currentPage > 0 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀ Назад", fmt.Sprintf("note:page:%d", currentPage-1)))
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", currentPage+1, totalPages), "note:noop"))
		if currentPage < totalPages-1 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("Далее ▶", fmt.Sprintf("note:page:%d", currentPage+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀ В меню", "note:back"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	return pageContent, &keyboard
}
