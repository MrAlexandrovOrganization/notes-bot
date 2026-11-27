package webapp

import (
	"net/http"
	"path"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
)

func (a *App) registerWebMarkdownRoutes(protected *http.ServeMux) {
	protected.HandleFunc("GET "+views.NoteImagePath, a.handleWebNoteImage)
}

func (a *App) handleWebNoteImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	query := r.URL.Query()
	note, ref := query.Get("note"), query.Get("reference")
	if path.Ext(note) != ".md" || ref == "" {
		http.NotFound(w, r)
		return
	}
	reader, ok := a.Core.(clients.NoteImageReader)
	if !ok {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	image, err := reader.ReadNoteImage(r.Context(), note, ref)
	switch {
	case status.Code(err) == codes.NotFound:
		http.NotFound(w, r)
	case err != nil:
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
	case image.GetMediaType() != "image/jpeg" || len(image.GetData()) == 0:
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	default:
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image.GetData())
	}
}
