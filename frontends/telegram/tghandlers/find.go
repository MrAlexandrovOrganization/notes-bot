package tghandlers

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

const (
	findSearchLimit  = 25
	notePreviewChars = 3500
	snippetMaxRunes  = 120
)

// HandleMenuFind opens the find-note prompt — user types a query, we search by
// name first and fall back to content search if name has too few hits.
func (a *App) HandleMenuFind(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateFindNoteInput
		u.FindQuery = ""
		u.FindResults = nil
		u.FindResultsPage = 0
		u.ActiveRelpath = ""
	})
	return replyToCallback(ctx, tgBot, query,
		tgfmt.Escape("🔎 Введите имя заметки или фразу для поиска:"), nil)
}

func (a *App) handleFindInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)

	q := strings.TrimSpace(text)
	if q == "" {
		sendText(ctx, tgBot, chatID, tgfmt.Escape("Пустой запрос. Попробуйте снова."), nil, true)
		return
	}

	hits, err := a.searchNotes(ctx, q, findSearchLimit)
	if err != nil {
		log.Error("search notes", zap.Error(err))
		sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Поиск временно недоступен."), nil, true)
		return
	}
	span.SetAttributes(attribute.Int("hits", len(hits)))

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateFindNoteResults
		u.FindQuery = q
		u.FindResults = hits
		u.FindResultsPage = 0
	})

	a.showFindResults(ctx, tgBot, chatID, 0, q, hits, nil)
}

// searchNotes runs name + content searches and merges results, name-matches first.
func (a *App) searchNotes(ctx context.Context, q string, limit int) ([]tgstates.SearchHit, error) {
	nameHits, err := a.Search.SearchByName(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("search by name: %w", err)
	}

	merged := make([]tgstates.SearchHit, 0, limit)
	seen := make(map[int64]struct{}, limit)
	add := func(h *clients.SearchHit) {
		if h == nil {
			return
		}
		if _, ok := seen[h.NoteID]; ok {
			return
		}
		seen[h.NoteID] = struct{}{}
		merged = append(merged, tgstates.SearchHit{
			NoteID:  h.NoteID,
			Relpath: h.Relpath,
			Name:    h.Name,
			Snippet: truncateSnippet(h.Snippet),
		})
	}
	for _, h := range nameHits {
		add(h)
	}
	if len(merged) < 3 {
		contentHits, err := a.Search.SearchByContent(ctx, q, limit)
		if err == nil {
			for _, h := range contentHits {
				if len(merged) >= limit {
					break
				}
				add(h)
			}
		}
	}
	return merged, nil
}

func truncateSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= snippetMaxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:snippetMaxRunes-1]) + "…"
}

// showFindResults renders the results list. If query is non-nil edits it,
// otherwise sends a new message.
func (a *App) showFindResults(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID int64, page int, q string, hits []tgstates.SearchHit, query *tgbotapi.CallbackQuery) {
	if len(hits) == 0 {
		body := tgfmt.Join(
			tgfmt.Escape("Ничего не нашлось по запросу "),
			tgfmt.Code(tgfmt.Escape(q)),
			tgfmt.Escape("."),
		)
		kb := tgkeyboards.FindResults(nil, 0)
		if query != nil {
			replyToCallback(ctx, tgBot, query, body, &kb)
		} else {
			sendText(ctx, tgBot, chatID, body, &kb, true)
		}
		return
	}

	startIdx := page * tgkeyboards.FindResultsPerPage
	endIdx := min(startIdx+tgkeyboards.FindResultsPerPage, len(hits))

	parts := []tgfmt.HTML{
		tgfmt.Bold(tgfmt.Escape("🔎 Результаты для ")),
		tgfmt.Code(tgfmt.Escape(q)),
		tgfmt.Raw("\n\n"),
	}
	for i := startIdx; i < endIdx; i++ {
		h := hits[i]
		parts = append(parts,
			tgfmt.Bold(tgfmt.Escape(fmt.Sprintf("%d. %s", i+1, h.Name))),
			tgfmt.Raw("\n"),
			tgfmt.Italic(tgfmt.Escape(h.Relpath)),
			tgfmt.Raw("\n"),
		)
		if h.Snippet != "" {
			parts = append(parts,
				tgfmt.Blockquote(tgfmt.Escape(h.Snippet)),
				tgfmt.Raw("\n"),
			)
		}
		parts = append(parts, tgfmt.Raw("\n"))
	}

	kb := tgkeyboards.FindResults(hits, page)
	body := tgfmt.Join(parts...)
	if query != nil {
		replyToCallback(ctx, tgBot, query, body, &kb)
	} else {
		sendText(ctx, tgBot, chatID, body, &kb, true)
	}
}

// HandleFindAction dispatches find:* callbacks.
func (a *App) handleFindAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("find.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "open":
		if len(parts) < 3 {
			return nil
		}
		var id int64
		if _, err := fmt.Sscanf(parts[2], "%d", &id); err != nil {
			return fmt.Errorf("parse id: %w", err)
		}
		return a.openFoundNote(ctx, tgBot, query, userID, id)

	case "page":
		if len(parts) < 3 {
			return nil
		}
		var page int
		if _, err := fmt.Sscanf(parts[2], "%d", &page); err != nil {
			return fmt.Errorf("parse page: %w", err)
		}
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return err
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.FindResultsPage = page })
		a.showFindResults(ctx, tgBot, query.Message.Chat.ID, page, uc.FindQuery, uc.FindResults, query)
		return nil

	case "back":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return err
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateFindNoteResults
			u.ActiveRelpath = ""
		})
		a.showFindResults(ctx, tgBot, query.Message.Chat.ID, uc.FindResultsPage, uc.FindQuery, uc.FindResults, query)
		return nil

	case "retry":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateFindNoteInput
		})
		return replyToCallback(ctx, tgBot, query,
			tgfmt.Escape("🔎 Введите новый запрос:"), nil)

	case "noop":
	}
	return nil
}

func (a *App) openFoundNote(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, id int64) error {
	log := applog.With(ctx, a.Logger)
	note, err := a.Search.GetNoteByID(ctx, id)
	if err != nil {
		log.Error("get note by id", zap.Error(err))
		return err
	}
	if note == nil {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Заметка не найдена."), nil)
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateViewNote
		u.ActiveRelpath = note.Relpath
	})

	preview := note.Content
	if !utf8.ValidString(preview) {
		preview = strings.ToValidUTF8(preview, "")
	}
	suffix := ""
	if utf8.RuneCountInString(preview) > notePreviewChars {
		r := []rune(preview)
		preview = string(r[:notePreviewChars])
		suffix = "\n\n…"
	}

	uc, _ := a.State.GetContext(ctx, userID)
	hasResults := uc != nil && len(uc.FindResults) > 0
	kb := tgkeyboards.NoteView(hasResults)

	text := tgfmt.Join(
		tgfmt.Bold(tgfmt.Escape("📄 "+note.Name)),
		tgfmt.Raw("\n"),
		tgfmt.Italic(tgfmt.Escape(note.Relpath)),
		tgfmt.Raw("\n\n"),
		tgfmt.Blockquote(tgfmt.Escape(preview+suffix)),
	)
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

// handleNoteAppendAction handles the "note:append" callback shown on an opened note.
func (a *App) handleNoteAppendAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		return err
	}
	if uc.ActiveRelpath == "" {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Сначала откройте заметку через поиск."), nil)
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateAppendToNoteInput })
	text := tgfmt.Join(
		tgfmt.Escape("✏️ Что добавить в "),
		tgfmt.Code(tgfmt.Escape(uc.ActiveRelpath)),
		tgfmt.Escape("?"),
	)
	return replyToCallback(ctx, tgBot, query, text, nil)
}

func (a *App) handleAppendToNoteInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil || uc.ActiveRelpath == "" {
		sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Контекст потерян, начните поиск заново."), nil, true)
		return
	}
	ok, err := a.Core.AppendToNoteByPath(ctx, uc.ActiveRelpath, text)
	if err != nil || !ok {
		log.Error("append to note by path", zap.String("relpath", uc.ActiveRelpath), zap.Error(err))
		sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Не удалось дописать заметку."), nil, true)
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateViewNote })

	confirm := tgfmt.Join(
		tgfmt.Escape("✅ Добавлено в "),
		tgfmt.Code(tgfmt.Escape(uc.ActiveRelpath)),
	)
	hasResults := len(uc.FindResults) > 0
	kb := tgkeyboards.NoteView(hasResults)
	sendText(ctx, tgBot, chatID, confirm, &kb, true)
	log.Info("appended to note", zap.String("relpath", uc.ActiveRelpath))
}
