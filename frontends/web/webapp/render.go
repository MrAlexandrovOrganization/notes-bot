package webapp

import (
	"net/http"

	"github.com/a-h/templ"
	"go.uber.org/zap"

	"notes-bot/internal/applog"
)

// render writes a templ.Component to the response, logging (but not panicking
// on) write failures — the client may have disconnected.
func (a *App) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		applog.With(r.Context(), a.Logger).Error("render template", zap.Error(err))
	}
}
