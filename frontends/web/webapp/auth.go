package webapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
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

// --- Session tokens ---
//
// Cookie value: "<sessionID>.<expiryUnix>.<hexHMAC>" where the HMAC covers
// sessionID and expiry using WEB_SESSION_SECRET. The random sessionID lets
// logout revoke individual sessions server-side (in-memory, sufficient for a
// single-user tool): a stolen cookie no longer survives a logout.

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func (a *App) signSession(id string, expiry time.Time) string {
	payload := id + "." + strconv.FormatInt(expiry.Unix(), 10)
	return payload + "." + a.sessionMAC(payload)
}

func (a *App) sessionMAC(payload string) string {
	mac := hmac.New(sha256.New, []byte(a.Cfg.WebSessionSecret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

type sessionToken struct {
	ID     string
	Expiry time.Time
}

// verifySession checks signature, expiry and revocation.
func (a *App) verifySession(value string) bool {
	tok, ok := a.parseSession(value)
	if !ok {
		return false
	}
	a.sessionsMu.Lock()
	_, revoked := a.revokedSessions[tok.ID]
	a.sessionsMu.Unlock()
	if revoked {
		return false
	}
	return time.Now().Before(tok.Expiry)
}

func (a *App) parseSession(value string) (sessionToken, bool) {
	parts := strings.SplitN(value, ".", 3)
	if len(parts) != 3 {
		return sessionToken{}, false
	}
	id, expStr, sig := parts[0], parts[1], parts[2]
	payload := id + "." + expStr
	if subtle.ConstantTimeCompare([]byte(sig), []byte(a.sessionMAC(payload))) != 1 {
		return sessionToken{}, false
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return sessionToken{}, false
	}
	return sessionToken{ID: id, Expiry: time.Unix(expUnix, 0)}, true
}

func cookieSecure(r *http.Request) bool {
	// Behind the reverse proxy Caddy sets X-Forwarded-Proto; direct local
	// access is plain HTTP. Both set/clear must use the same logic.
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.signSession(newSessionID(), expiry),
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// revokeSession marks the current session ID as revoked until its expiry.
func (a *App) revokeSession(r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return
	}
	tok, ok := a.parseSession(cookie.Value)
	if !ok {
		return
	}
	now := time.Now()
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()
	if a.revokedSessions == nil {
		a.revokedSessions = make(map[string]time.Time)
	}
	for id, exp := range a.revokedSessions {
		if now.After(exp) {
			delete(a.revokedSessions, id)
		}
	}
	a.revokedSessions[tok.ID] = tok.Expiry
}

// sameOrigin verifies that state-changing requests originate from this site
// (CSRF defense in depth on top of SameSite=Strict cookies). Requests without
// Origin/Referer (curl, native apps) are allowed through.
func sameOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return nil
	}
	scheme := "http"
	if cookieSecure(r) {
		scheme = "https"
	}
	expected := scheme + "://" + r.Host
	if !strings.HasPrefix(origin+"/", expected+"/") && origin != expected {
		return fmt.Errorf("cross-origin request from %q rejected", origin)
	}
	return nil
}

// requireAuth redirects to /login when the session cookie is missing or
// invalid, refreshes the cookie (sliding TTL), enforces same-origin on
// mutating requests, and revokes logged-out sessions.
func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if err := sameOrigin(r); err != nil {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !a.verifySession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		a.setSessionCookie(w, r)
		next.ServeHTTP(w, r)
	})
}

// --- Login rate limiting ---

const (
	loginMaxFailures   = 10
	loginFailureWindow = 5 * time.Minute
)

// tooManyLoginAttempts reports whether the remote IP exhausted its failure budget.
func (a *App) tooManyLoginAttempts(ip string) bool {
	now := time.Now()
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	recent := a.loginFailures[ip][:0]
	for _, t := range a.loginFailures[ip] {
		if now.Sub(t) < loginFailureWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(a.loginFailures, ip)
		return false
	}
	a.loginFailures[ip] = recent
	return len(recent) >= loginMaxFailures
}

func (a *App) recordLoginFailure(ip string) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.loginFailures == nil {
		a.loginFailures = make(map[string][]time.Time)
	}
	a.loginFailures[ip] = append(a.loginFailures[ip], time.Now())
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, views.Login(""))
}

func (a *App) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.render(w, r, views.Login("Некорректная форма"))
		return
	}
	ip := clientIP(r)
	if a.tooManyLoginAttempts(ip) {
		http.Error(w, "Слишком много попыток входа. Подождите несколько минут.", http.StatusTooManyRequests)
		return
	}
	password := r.PostFormValue("password")
	if subtle.ConstantTimeCompare([]byte(password), []byte(a.Cfg.WebPassword)) != 1 {
		a.recordLoginFailure(ip)
		a.render(w, r, views.Login("Неверный пароль"))
		return
	}
	a.setSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.revokeSession(r)
	clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
