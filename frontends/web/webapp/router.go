package webapp

import (
	"embed"
	"net/http"

	"go.uber.org/zap"

	"notes-bot/internal/applog"
)

//go:embed static
var staticFS embed.FS

// NewRouter builds the full HTTP route table. Every route except /login and
// /static/* is gated behind requireAuth.
func (a *App) NewRouter() http.Handler {
	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", a.handleIndex)
	protected.HandleFunc("GET /logout", a.handleLogout)

	a.registerDayRoutes(protected)
	a.registerCalendarRoutes(protected)
	a.registerReminderRoutes(protected)
	a.registerSearchRoutes(protected)
	a.registerAskRoutes(protected)
	a.registerBrowseRoutes(protected)
	a.registerSmartRoutes(protected)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLoginSubmit)
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.Handle("/", a.requireAuth(protected))

	return a.logRequests(mux)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/day", http.StatusSeeOther)
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		applog.With(r.Context(), a.Logger).Info("http request",
			zap.String("method", r.Method), zap.String("path", r.URL.Path))
	})
}
