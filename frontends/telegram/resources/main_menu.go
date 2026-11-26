package resources

import "notes-bot/frontends/telegram/tgfmt"

const (
	OpenNotesMiniApp     = "Открыть Notes"
	MainMenuWelcome      = "👋 Добро пожаловать!"
	MainMenuDateSelected = "✅ Дата выбрана."
)

func MainMenuPrompt(activeDate string) tgfmt.HTML {
	// Telegram for macOS expands the bubble for inline keyboards when the text
	// layout is narrower than 200 points. Put the date on its own line.
	return tgfmt.Join(
		tgfmt.Escape("📅 Активная дата:\n"),
		tgfmt.Code(tgfmt.Escape(activeDate)),
		tgfmt.Escape("\n\nВыберите действие:"),
	)
}
