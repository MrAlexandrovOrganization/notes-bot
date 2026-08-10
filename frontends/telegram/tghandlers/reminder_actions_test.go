package tghandlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAcknowledgedReminderText_PreservesTitleHighlighting(t *testing.T) {
	got := acknowledgedReminderText("Купить <молоко> & хлеб")
	assert.Equal(t, "🔔 Напоминание: <blockquote>Купить &lt;молоко&gt; &amp; хлеб</blockquote>\n\n✅ Принято!", string(got))
}
