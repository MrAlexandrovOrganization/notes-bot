// Package tgfmt provides composable HTML formatting helpers for Telegram's HTML parse mode.
//
// The HTML type is a safe wrapper for Telegram HTML text. Raw user input must
// always be converted with Escape before combining with formatting tags:
//
//	Bold(Escape("user text"))                    // <b>user text</b>
//	Bold(Italic(Escape("text")))                 // <b><i>text</i></b>
//	Join(Escape("label: "), Code(Escape(value))) // label: <code>value</code>
package tgfmt

import (
	"html"
	"strings"
)

// HTML represents HTML-formatted text safe for Telegram's HTML parse mode.
// Values must not be constructed from untrusted input without Escape.
type HTML string

// String returns the underlying HTML string for Telegram API calls.
func (h HTML) String() string { return string(h) }

// Escape converts a plain string to safe HTML by escaping &, < and >.
// All dynamic or user-provided strings must pass through Escape.
func Escape(s string) HTML {
	return HTML(html.EscapeString(s))
}

// Raw marks a trusted string literal as safe HTML without any escaping.
// Only use this for string constants you fully control.
func Raw(s string) HTML {
	return HTML(s)
}

// Join concatenates HTML fragments into one HTML value.
func Join(parts ...HTML) HTML {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(string(p))
	}
	return HTML(b.String())
}

// Formatting tags — each wraps its argument in the corresponding HTML element.

func Bold(h HTML) HTML       { return "<b>" + h + "</b>" }
func Italic(h HTML) HTML     { return "<i>" + h + "</i>" }
func Underline(h HTML) HTML  { return "<u>" + h + "</u>" }
func Strike(h HTML) HTML     { return "<s>" + h + "</s>" }
func Code(h HTML) HTML       { return "<code>" + h + "</code>" }
func Pre(h HTML) HTML        { return "<pre>" + h + "</pre>" }
func Spoiler(h HTML) HTML    { return "<tg-spoiler>" + h + "</tg-spoiler>" }
func Blockquote(h HTML) HTML { return "<blockquote>" + h + "</blockquote>" }

// Link creates an HTML hyperlink. The URL is escaped automatically.
func Link(text HTML, url string) HTML {
	return HTML(`<a href="` + html.EscapeString(url) + `">` + string(text) + `</a>`)
}
