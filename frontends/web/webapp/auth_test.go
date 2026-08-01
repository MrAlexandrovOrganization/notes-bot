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
	value := a.signSession(time.Now().Add(time.Hour))
	assert.True(t, a.verifySession(value))
}

func TestVerifySession_Expired(t *testing.T) {
	a := testApp(t)
	value := a.signSession(time.Now().Add(-time.Hour))
	assert.False(t, a.verifySession(value))
}

func TestVerifySession_TamperedSignature(t *testing.T) {
	a := testApp(t)
	value := a.signSession(time.Now().Add(time.Hour))
	tampered := value[:len(value)-1] + "0"
	assert.False(t, a.verifySession(tampered))
}

func TestVerifySession_WrongSecret(t *testing.T) {
	a := testApp(t)
	value := a.signSession(time.Now().Add(time.Hour))

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
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(time.Now().Add(time.Hour))})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
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
