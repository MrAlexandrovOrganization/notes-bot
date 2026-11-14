package englishtutor

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRussianThoughtKeepsUserWordingSeparateFromAttempt(t *testing.T) {
	for _, tc := range []struct {
		input, source string
		ok            bool
	}{
		{"Я потратил полчаса, пытаясь это исправить", "Я потратил полчаса, пытаясь это исправить", true},
		{"Я хочу место у окна.\nМой вариант: I want a window seat.", "Я хочу место у окна.", true},
		{"Я хочу место у окна. Мой вариант: I want a window seat.", "Я хочу место у окна.", true},
		{"Я хочу место у окна.\nI want a window seat.", "Я хочу место у окна.", true},
		{"Я хочу посетить New York City.", "Я хочу посетить New York City.", true},
		{"Я хочу кофе. I want coffee.", "", false},
		{"I want coffee.", "", false},
		{strings.Repeat("я", 601), "", false},
	} {
		name := []rune(tc.input)
		t.Run(string(name[:min(20, len(name))]), func(t *testing.T) {
			source, ok := RussianThought(tc.input)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.source, source)
		})
	}
}

func TestCorrectionAcceptsVoicePunctuationWithoutChangingOrdinaryThought(t *testing.T) {
	for _, prefix := range []string{"Исправление:", "исправление.", "ИСПРАВЛЕНИЕ —"} {
		text, ok := CorrectedInput(prefix + " Я потратил час")
		require.True(t, ok)
		require.Equal(t, "Я потратил час", text)
	}
	_, ok := CorrectedInput("Исправление заняло час")
	require.False(t, ok)
}
