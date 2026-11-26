package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMiniAppSharesWebSession(t *testing.T) {
	app := testApp(t)
	app.Cfg.RootID = 123
	app.Cfg.TelegramBotToken = "123456:synthetic-preview"
	router := app.NewRouter()
	const origin = "https://notes.example.org"

	send := func(method, route, body, from string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, origin+route, strings.NewReader(body))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", from)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	view := send(http.MethodGet, "/mini", "", "", nil)
	require.Equal(t, http.StatusOK, view.Code)
	require.Contains(t, view.Body.String(), "telegram-web-app.js")
	require.Equal(t, "no-store", view.Header().Get("Cache-Control"))
	for _, path := range []string{"/day", "/english", "/reminders"} {
		require.Equal(t, "/login", send(http.MethodGet, path, "", "", nil).Header().Get("Location"))
	}

	now := time.Now()
	makeBody := func(owner string, signedAt time.Time) string {
		fields := url.Values{"user": {owner}, "auth_date": {strconv.FormatInt(signedAt.Unix(), 10)}, "query_id": {"synthetic-query"}}
		return url.Values{"init_data": {signTelegramFixture(app.Cfg.TelegramBotToken, fields)}}.Encode()
	}
	owner := `{"id":123,"first_name":"Synthetic"}`
	accepted := makeBody(owner, now)
	require.Equal(t, http.StatusForbidden, send(http.MethodPost, "/mini/session", accepted, "https://other.example.org", nil).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/mini/session", makeBody(`{"id":456}`, now), origin, nil).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/mini/session", makeBody(owner, now.Add(-10*time.Minute)), origin, nil).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/mini/session", makeBody(owner, now.Add(time.Minute)), origin, nil).Code)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/mini/session", accepted+"%26auth_date%3D1", origin, nil).Code)

	reply := send(http.MethodPost, "/mini/session", accepted, origin, nil)
	require.Equal(t, http.StatusNoContent, reply.Code)
	cookies := reply.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].Secure && cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, "/mini/session", accepted, origin, nil).Code)

	home := send(http.MethodGet, "/", "", "", cookies[0])
	require.Equal(t, "/day", home.Header().Get("Location"))
	require.Equal(t, "no-store", home.Header().Get("Cache-Control"))
	reopen := send(http.MethodGet, "/mini", "", "", cookies[0])
	require.Contains(t, reopen.Body.String(), `data-signed-in="true"`)
}

func signTelegramFixture(token string, fields url.Values) string {
	lines := make([]string, 0, len(fields))
	for key, value := range fields {
		lines = append(lines, key+"="+value[0])
	}
	slices.Sort(lines)
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte(token))
	signature := hmac.New(sha256.New, key.Sum(nil))
	_, _ = signature.Write([]byte(strings.Join(lines, "\n")))
	fields.Set("hash", hex.EncodeToString(signature.Sum(nil)))
	return fields.Encode()
}
