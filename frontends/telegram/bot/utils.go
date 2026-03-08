package bot

import "regexp"

var mdV2EscapeRe = regexp.MustCompile(`([_*\[\]()~` + "`" + `>#\+\-=|{}.!])`)

// EscapeMarkdownV2 escapes special MarkdownV2 characters.
func EscapeMarkdownV2(text string) string {
	return mdV2EscapeRe.ReplaceAllString(text, `\$1`)
}
