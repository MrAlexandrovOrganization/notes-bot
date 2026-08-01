package webapp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
)

const (
	searchLimit      = 25
	notePreviewChars = 3500
	snippetMaxRunes  = 120
)

func (a *App) registerSearchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", a.handleSearch)
	mux.HandleFunc("GET /search/note/{id}", a.handleSearchOpenNote)
	mux.HandleFunc("POST /search/note/{id}/append", a.handleSearchAppendNote)
}

// searchNotes mirrors find.go's searchNotes: name search first, falling back
// to content search when there are fewer than 3 hits.
func (a *App) searchNotes(ctx context.Context, q string, limit int) ([]views.SearchHitData, error) {
	nameHits, err := a.Search.SearchByName(ctx, q, limit)
	if err != nil {
		return nil, err
	}

	merged := make([]views.SearchHitData, 0, limit)
	seen := make(map[int64]struct{}, limit)
	add := func(h *clients.SearchHit) {
		if h == nil {
			return
		}
		if _, ok := seen[h.NoteID]; ok {
			return
		}
		seen[h.NoteID] = struct{}{}
		merged = append(merged, views.SearchHitData{
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

func truncatePreview(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	if utf8.RuneCountInString(s) <= notePreviewChars {
		return s
	}
	r := []rune(s)
	return string(r[:notePreviewChars]) + "\n\n…"
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var hits []views.SearchHitData
	if q != "" {
		var err error
		hits, err = a.searchNotes(r.Context(), q, searchLimit)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	a.render(w, r, views.Search(views.SearchData{Query: q, Hits: hits}))
}

func (a *App) handleSearchOpenNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.formError(w, r, "Некорректный идентификатор заметки")
		return
	}
	note, err := a.Search.GetNoteByID(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if note == nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, views.SearchNote(views.SearchNoteData{
		ID:      note.ID,
		Name:    note.Name,
		Relpath: note.Relpath,
		Content: truncatePreview(note.Content),
	}))
}

func (a *App) handleSearchAppendNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.formError(w, r, "Некорректный идентификатор заметки")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	text := r.PostFormValue("text")
	relpath := r.PostFormValue("relpath")
	if text == "" || relpath == "" {
		a.formError(w, r, "Текст не может быть пустым")
		return
	}
	if _, err := a.Core.AppendToNoteByPath(r.Context(), relpath, text); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/search/note/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}
