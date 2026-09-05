package webapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/web/config"
)

func testApp(t *testing.T) *App {
	t.Helper()
	return &App{
		Cfg: &config.Config{
			WebPassword:      "s3cret",
			WebSessionSecret: "test-signing-secret",
		},
		Logger: zap.NewNop(),
	}
}

func TestSignVerifySession_RoundTrip(t *testing.T) {
	a := testApp(t)
	value := a.signSession(newSessionID(), time.Now().Add(time.Hour))
	assert.True(t, a.verifySession(value))
}

func TestVerifySession_Expired(t *testing.T) {
	a := testApp(t)
	value := a.signSession(newSessionID(), time.Now().Add(-time.Hour))
	assert.False(t, a.verifySession(value))
}

func TestVerifySession_TamperedSignature(t *testing.T) {
	a := testApp(t)
	value := a.signSession(newSessionID(), time.Now().Add(time.Hour))
	replacement := "0"
	if value[len(value)-1] == '0' {
		replacement = "1"
	}
	tampered := value[:len(value)-1] + replacement
	assert.False(t, a.verifySession(tampered))
}

func TestVerifySession_WrongSecret(t *testing.T) {
	a := testApp(t)
	value := a.signSession(newSessionID(), time.Now().Add(time.Hour))

	other := testApp(t)
	other.Cfg.WebSessionSecret = "different-secret"
	assert.False(t, other.verifySession(value))
}

func TestVerifySession_Malformed(t *testing.T) {
	a := testApp(t)
	assert.False(t, a.verifySession(""))
	assert.False(t, a.verifySession("not-a-valid-cookie"))
}

func TestRequireAuth_RedirectsWithoutCookie(t *testing.T) {
	a := testApp(t)
	handler := a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid session")
	}))

	req := httptest.NewRequest(http.MethodGet, "/day", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/login", rec.Header().Get("Location"))
}

func TestRequireAuth_AllowsValidCookie(t *testing.T) {
	a := testApp(t)
	called := false
	handler := a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/day", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
}

func TestRequireAuth_SlidingRefreshKeepsSessionID(t *testing.T) {
	a := testApp(t)
	id := newSessionID()
	value := a.signSession(id, time.Now().Add(time.Hour))
	handler := a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/day", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	refreshed := rec.Result().Cookies()[0].Value
	tok, ok := a.parseSession(refreshed)
	require.True(t, ok)
	assert.Equal(t, id, tok.ID)

	a.revokeSession(req)
	assert.False(t, a.verifySession(refreshed), "logout must revoke refreshed cookies")
}

func TestHandleLoginSubmit_WrongPassword(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.PostForm = map[string][]string{"password": {"wrong"}}
	rec := httptest.NewRecorder()

	a.handleLoginSubmit(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Неверный пароль")
	assert.Empty(t, rec.Result().Cookies())
}

func TestHandleLoginSubmit_CorrectPassword(t *testing.T) {
	a := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.PostForm = map[string][]string{"password": {"s3cret"}}
	rec := httptest.NewRecorder()

	a.handleLoginSubmit(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Len(t, rec.Result().Cookies(), 1)
	assert.Equal(t, sessionCookieName, rec.Result().Cookies()[0].Name)
}

func TestVerifySession_RevokedAfterLogout(t *testing.T) {
	a := testApp(t)
	value := a.signSession(newSessionID(), time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})

	a.revokeSession(req)
	assert.False(t, a.verifySession(value), "revoked session must not verify")
	// A different session with the same secret still works.
	assert.True(t, a.verifySession(a.signSession(newSessionID(), time.Now().Add(time.Hour))))
}

func TestRequireAuth_RejectsCrossOriginPost(t *testing.T) {
	a := testApp(t)
	handler := a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/day/rating", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))})
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequireAuth_AllowsSameOriginPost(t *testing.T) {
	a := testApp(t)
	called := false
	handler := a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest(http.MethodPost, "/day/rating", nil)
	req.Host = "notes.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))})
	req.Header.Set("Origin", "https://notes.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, called)
}

func TestLoginRateLimit(t *testing.T) {
	a := testApp(t)
	ip := "10.0.0.1"
	for i := 0; i < loginMaxFailures; i++ {
		assert.False(t, a.tooManyLoginAttempts(ip))
		a.recordLoginFailure(ip)
	}
	assert.True(t, a.tooManyLoginAttempts(ip), "10th failure must trip the limiter")
	assert.False(t, a.tooManyLoginAttempts("10.0.0.2"), "other IPs unaffected")

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = ip + ":12345"
	req.PostForm = map[string][]string{"password": {"wrong"}}
	rec := httptest.NewRecorder()
	a.handleLoginSubmit(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}
