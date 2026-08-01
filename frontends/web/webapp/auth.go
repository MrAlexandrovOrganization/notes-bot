package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"notes-bot/frontends/web/views"
)

const (
	sessionCookieName = "web_session"
	sessionTTL        = 30 * 24 * time.Hour
)

// signSession returns a cookie value of the form "<expiryUnix>.<hexHMAC>",
// where the HMAC covers the expiry using Cfg.WebSessionSecret. Stateless —
// no server-side session store needed for a single-user tool.
func (a *App) signSession(expiry time.Time) string {
	expStr := strconv.FormatInt(expiry.Unix(), 10)
	return expStr + "." + a.sessionMAC(expStr)
}

func (a *App) sessionMAC(expStr string) string {
	mac := hmac.New(sha256.New, []byte(a.Cfg.WebSessionSecret))
	mac.Write([]byte(expStr))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifySession checks the cookie value's signature and expiry.
func (a *App) verifySession(value string) bool {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	expStr, sig := parts[0], parts[1]

	expected := a.sessionMAC(expStr)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return false
	}

	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Before(time.Unix(expUnix, 0))
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.signSession(expiry),
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// requireAuth redirects to /login when the session cookie is missing or invalid,
// and refreshes the cookie (sliding TTL) on every valid request.
func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !a.verifySession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		a.setSessionCookie(w, r)
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, views.Login(""))
}

func (a *App) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.render(w, r, views.Login("Некорректная форма"))
		return
	}
	password := r.PostFormValue("password")
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.Cfg.WebPassword)) != 1 {
		a.render(w, r, views.Login("Неверный пароль"))
		return
	}
	a.setSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
