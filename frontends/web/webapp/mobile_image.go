package webapp

import (
	"net/http"
	"notes-bot/frontends/telegram/clients"
)

func (a *App) mobileImage(w http.ResponseWriter, r *http.Request) {
	reader, ok := a.Core.(clients.NoteImageReader)
	if !ok {
		mobileError(w, 503)
		return
	}
	image, err := reader.ReadNoteImage(r.Context(), r.URL.Query().Get("note"), r.URL.Query().Get("reference"))
	if err != nil {
		mobileFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", image.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(image.Data)
}
