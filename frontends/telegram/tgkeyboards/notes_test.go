package tgkeyboards

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
)

func TestBrowseFilePaginationKeepsAllContent(t *testing.T) {
	content := strings.Repeat("я", noteCharsPerPage) + "конец"

	firstPage, firstKeyboard := BrowseFilePagination(content, 0)
	secondPage, secondKeyboard := BrowseFilePagination(content, 1)

	assert.Contains(t, firstPage, "[Страница 1/2]")
	assert.Equal(t, strings.Repeat("я", noteCharsPerPage), strings.TrimPrefix(firstPage, "[Страница 1/2]\n\n"))
	assert.Equal(t, "[Страница 2/2]\n\nконец", secondPage)

	firstCallbacks := callbacksFromKeyboard(firstKeyboard)
	secondCallbacks := callbacksFromKeyboard(secondKeyboard)
	assert.Contains(t, firstCallbacks, "browse:file_page:1")
	assert.Contains(t, secondCallbacks, "browse:file_page:0")
	assert.Contains(t, secondCallbacks, "browse:file_back")
}

func TestFoundNotePaginationUsesSearchNavigation(t *testing.T) {
	content := strings.Repeat("x", noteCharsPerPage+1)
	_, keyboard := FoundNotePagination(content, 0, true)
	callbacks := callbacksFromKeyboard(keyboard)

	assert.Contains(t, callbacks, "find:note_page:1")
	assert.Contains(t, callbacks, "find:noop")
	assert.Contains(t, callbacks, "note:append")
	assert.Contains(t, callbacks, "find:back")
	assert.Contains(t, callbacks, "menu:back")
}

func TestDailyNotePaginationKeepsExistingCallbacks(t *testing.T) {
	_, keyboard := NotePagination(strings.Repeat("x", noteCharsPerPage+1), 0)
	callbacks := callbacksFromKeyboard(keyboard)

	assert.Contains(t, callbacks, "note:page:1")
	assert.Contains(t, callbacks, "note:noop")
	assert.Contains(t, callbacks, "note:back")
}

func callbacksFromKeyboard(keyboard *tgbotapi.InlineKeyboardMarkup) []string {
	var callbacks []string
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData != nil {
				callbacks = append(callbacks, *button.CallbackData)
			}
		}
	}
	return callbacks
}
