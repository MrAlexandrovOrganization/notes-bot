package tghandlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEscapeMarkdownV2_PlainText(t *testing.T) {
	assert.Equal(t, "hello world", EscapeMarkdownV2("hello world"))
}

func TestEscapeMarkdownV2_SpecialChars(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"_", `\_`},
		{"*", `\*`},
		{"[", `\[`},
		{"]", `\]`},
		{"(", `\(`},
		{")", `\)`},
		{"~", `\~`},
		{">", `\>`},
		{"#", `\#`},
		{"+", `\+`},
		{"-", `\-`},
		{"=", `\=`},
		{"|", `\|`},
		{"{", `\{`},
		{"}", `\}`},
		{".", `\.`},
		{"!", `\!`},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.expected, EscapeMarkdownV2(tc.input), "input: %q", tc.input)
	}
}

func TestEscapeMarkdownV2_Mixed(t *testing.T) {
	input := "Hello, world! Price: $5.00 (tax+fees)"
	result := EscapeMarkdownV2(input)
	assert.Contains(t, result, `\!`)
	assert.Contains(t, result, `\.`)
	assert.Contains(t, result, `\(`)
	assert.Contains(t, result, `\)`)
	assert.Contains(t, result, `\+`)
}

func TestEscapeMarkdownV2_Empty(t *testing.T) {
	assert.Equal(t, "", EscapeMarkdownV2(""))
}

func TestEscapeMarkdownV2_AlreadyEscaped(t *testing.T) {
	// Backslash itself is not in the escape set — should pass through unchanged.
	assert.Equal(t, `\`, EscapeMarkdownV2(`\`))
}
