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

// searchNotes delegates retrieval selection to the product-level search API.
func (a *App) searchNotes(ctx context.Context, q string, limit int) ([]views.SearchHitData, error) {
	hits, err := a.Search.FindNotes(ctx, q, limit, clients.SearchOptions{})
	if err != nil {
		return nil, err
	}

	merged := make([]views.SearchHitData, 0, limit)
	for _, h := range hits {
		if h == nil {
			continue
		}
		merged = append(merged, views.SearchHitData{
			NoteID:  h.NoteID,
			Relpath: h.Relpath,
			Name:    h.Name,
			Snippet: truncateSnippet(h.Snippet),
		})
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
