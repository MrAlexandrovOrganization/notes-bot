package webapp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeNoteContentDoesNotTruncate(t *testing.T) {
	content := strings.Repeat("длинная заметка ", 500)
	assert.Equal(t, content, normalizeNoteContent(content))
}

func TestNormalizeNoteContentRemovesInvalidUTF8(t *testing.T) {
	assert.Equal(t, "beforeafter", normalizeNoteContent("before\xffafter"))
}
