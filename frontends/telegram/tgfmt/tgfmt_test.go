package tgfmt_test

import (
	"testing"

	"notes-bot/frontends/telegram/tgfmt"

	"github.com/stretchr/testify/assert"
)

func TestEscape_PlainText(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("hello world"), tgfmt.Escape("hello world"))
}

func TestEscape_HTMLChars(t *testing.T) {
	cases := []struct {
		input    string
		expected tgfmt.HTML
	}{
		{"<", "&lt;"},
		{">", "&gt;"},
		{"&", "&amp;"},
		{"<b>bold</b>", "&lt;b&gt;bold&lt;/b&gt;"},
		{"a & b", "a &amp; b"},
		{"<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;/script&gt;"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.expected, tgfmt.Escape(tc.input), "input: %q", tc.input)
	}
}

func TestEscape_Empty(t *testing.T) {
	assert.Equal(t, tgfmt.HTML(""), tgfmt.Escape(""))
}

func TestRaw(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<b>test</b>"), tgfmt.Raw("<b>test</b>"))
}

func TestJoin(t *testing.T) {
	assert.Equal(t, tgfmt.HTML(""), tgfmt.Join())
	assert.Equal(t, tgfmt.HTML("a"), tgfmt.Join("a"))
	assert.Equal(t, tgfmt.HTML("abc"), tgfmt.Join("a", "b", "c"))
}

func TestBold(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<b>text</b>"), tgfmt.Bold(tgfmt.Escape("text")))
}

func TestItalic(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<i>text</i>"), tgfmt.Italic(tgfmt.Escape("text")))
}

func TestCode(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<code>x &amp; y</code>"), tgfmt.Code(tgfmt.Escape("x & y")))
}

func TestPre(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<pre>line1\nline2</pre>"), tgfmt.Pre(tgfmt.Escape("line1\nline2")))
}

func TestNesting(t *testing.T) {
	result := tgfmt.Bold(tgfmt.Italic(tgfmt.Escape("text")))
	assert.Equal(t, tgfmt.HTML("<b><i>text</i></b>"), result)
}

func TestJoinWithFormatting(t *testing.T) {
	result := tgfmt.Join(
		tgfmt.Escape("label: "),
		tgfmt.Code(tgfmt.Escape("value")),
	)
	assert.Equal(t, tgfmt.HTML("label: <code>value</code>"), result)
}

func TestLink(t *testing.T) {
	result := tgfmt.Link(tgfmt.Escape("click here"), "https://example.com")
	assert.Equal(t, tgfmt.HTML(`<a href="https://example.com">click here</a>`), result)
}

func TestLink_URLEscaped(t *testing.T) {
	result := tgfmt.Link(tgfmt.Escape("link"), "https://example.com?a=1&b=2")
	assert.Equal(t, tgfmt.HTML(`<a href="https://example.com?a=1&amp;b=2">link</a>`), result)
}

func TestStrike(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<s>text</s>"), tgfmt.Strike(tgfmt.Escape("text")))
}

func TestUnderline(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<u>text</u>"), tgfmt.Underline(tgfmt.Escape("text")))
}

func TestSpoiler(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<tg-spoiler>text</tg-spoiler>"), tgfmt.Spoiler(tgfmt.Escape("text")))
}

func TestBlockquote(t *testing.T) {
	assert.Equal(t, tgfmt.HTML("<blockquote>text</blockquote>"), tgfmt.Blockquote(tgfmt.Escape("text")))
}
