package webapp

import "net/http"

// registerWebVoiceRoutes is mounted inside the cookie and same-origin middleware.
func (a *App) registerWebVoiceRoutes(mux *http.ServeMux) {
	a.registerVoice(mux, "/voice/{id}", true)
}
