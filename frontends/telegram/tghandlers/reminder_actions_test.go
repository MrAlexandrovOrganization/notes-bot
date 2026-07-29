package tghandlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractReminderTitle_SimpleText(t *testing.T) {
	msg := "🔔 Напоминание: <blockquote>Позвонить маме</blockquote>"
	got := extractReminderTitle(msg)
	assert.Equal(t, "Позвонить маме", got)
}

func TestExtractReminderTitle_HTMLSpecialChars(t *testing.T) {
	msg := "🔔 Напоминание: <blockquote>Купить &lt;молоко&gt; &amp; хлеб</blockquote>"
	got := extractReminderTitle(msg)
	assert.Equal(t, "Купить <молоко> & хлеб", got)
}

func TestExtractReminderTitle_Cyrillic(t *testing.T) {
	msg := "🔔 Напоминание: <blockquote>Завтра в 9 утра планёрка</blockquote>"
	got := extractReminderTitle(msg)
	assert.Equal(t, "Завтра в 9 утра планёрка", got)
}

func TestExtractReminderTitle_WithEmoji(t *testing.T) {
	msg := "🔔 Напоминание: <blockquote>🎉 День рождения!</blockquote>"
	got := extractReminderTitle(msg)
	assert.Equal(t, "🎉 День рождения!", got)
}

func TestExtractReminderTitle_NoPrefix(t *testing.T) {
	msg := "<blockquote>Just title</blockquote>"
	got := extractReminderTitle(msg)
	assert.Equal(t, "Just title", got)
}

func TestExtractReminderTitle_NoBlockquote(t *testing.T) {
	msg := "🔔 Напоминание: Plain text"
	got := extractReminderTitle(msg)
	assert.Equal(t, "Plain text", got)
}

func TestExtractReminderTitle_Empty(t *testing.T) {
	assert.Equal(t, "", extractReminderTitle(""))
}
