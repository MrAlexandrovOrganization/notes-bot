package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"notes-bot/frontends/web/views"
)

const telegramLoginTTL = 5 * time.Minute

func (a *App) handleMiniApp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, err := r.Cookie(sessionCookieName)
	a.render(w, r, views.LoginPage("", true, err == nil && a.verifySession(session.Value)))
}

func (a *App) handleTelegramLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.Cfg.TelegramBotToken == "" || sameOrigin(r) != nil {
		http.Error(w, "Вход через Telegram недоступен", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректные данные входа", http.StatusBadRequest)
		return
	}

	now := time.Now()
	hash, expires, valid := verifyTelegramLogin(r.PostForm.Get("init_data"), a.Cfg.TelegramBotToken, a.Cfg.RootID, now)
	if !valid || !a.consumeTelegramLogin(hash, expires, now) {
		http.Error(w, "Вход через Telegram отклонён", http.StatusUnauthorized)
		return
	}

	a.setSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) consumeTelegramLogin(hash string, expiry, now time.Time) bool {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.telegramLogins == nil {
		a.telegramLogins = make(map[string]time.Time)
	}
	for used, until := range a.telegramLogins {
		if !now.Before(until) {
			delete(a.telegramLogins, used)
		}
	}
	if _, used := a.telegramLogins[hash]; used {
		return false
	}
	a.telegramLogins[hash] = expiry
	return true
}

func verifyTelegramLogin(raw, token string, owner int64, now time.Time) (string, time.Time, bool) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", time.Time{}, false
	}
	hash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(hash) != sha256.Size {
		return "", time.Time{}, false
	}
	fields := make([]string, 0, len(values))
	for key, entry := range values {
		if len(entry) != 1 || strings.ContainsAny(key, "=\r\n") || strings.ContainsAny(entry[0], "\r\n") {
			return "", time.Time{}, false
		}
		if key != "hash" {
			fields = append(fields, key+"="+entry[0])
		}
	}
	slices.Sort(fields)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(fields, "\n")))
	if !hmac.Equal(hash, mac.Sum(nil)) {
		return "", time.Time{}, false
	}
	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	signedAt := time.Unix(authDate, 0)
	expires := signedAt.Add(telegramLoginTTL)
	if err != nil || signedAt.After(now.Add(30*time.Second)) || !now.Before(expires) {
		return "", time.Time{}, false
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || owner <= 0 || user.ID != owner {
		return "", time.Time{}, false
	}
	return hex.EncodeToString(hash), expires, true
}
