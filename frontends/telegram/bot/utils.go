package bot

import (
	"context"
	"notes_bot/internal/telemetry"
	"regexp"
)

var mdV2EscapeRe = regexp.MustCompile(`([_*\[\]()~` + "`" + `>#\+\-=|{}.!])`)

// EscapeMarkdownV2 escapes special MarkdownV2 characters.
func EscapeMarkdownV2(ctx context.Context, text string) string {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	return mdV2EscapeRe.ReplaceAllString(text, `\$1`)
}
