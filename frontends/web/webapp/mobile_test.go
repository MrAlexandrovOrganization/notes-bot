package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mobileCore struct {
	*fakeCore
	seen    map[string]string
	appends int
}

func (f *mobileCore) AppendToNoteOnce(_ context.Context, date, text, id string) (bool, error) {
	return f.appendOnce(date, text, id)
}
func (f *mobileCore) AppendToNoteByPathOnce(_ context.Context, path, text, id string) (bool, error) {
	return f.appendOnce(path, text, id)
}
func (f *mobileCore) appendOnce(target, text, id string) (bool, error) {
	value := target + "\x00" + text
	if previous, ok := f.seen[id]; ok {
		if previous != value {
			return false, status.Error(codes.AlreadyExists, "private content must not escape")
		}
		return true, nil
	}
	f.seen[id] = value
	f.appends++
	return true, nil
}

func mobileRequest(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestMobileAuthIsolationAndLogout(t *testing.T) {
	a := testApp(t)
	a.Core = newFakeCore()
	h := a.NewMobileRouter()
	require.Equal(t, 401, mobileRequest(h, "GET", "/mobile/v1/bootstrap", "", "").Code)
	require.Equal(t, 401, mobileRequest(h, "POST", "/mobile/v1/session", "", `{"password":"wrong"}`).Code)
	login := mobileRequest(h, "POST", "/mobile/v1/session", "", `{"password":"s3cret"}`)
	require.Equal(t, 200, login.Code)
	var response map[string]string
	require.NoError(t, json.Unmarshal(login.Body.Bytes(), &response))
	token := response["token"]
	require.NotEmpty(t, token)
	require.Equal(t, 200, mobileRequest(h, "GET", "/mobile/v1/bootstrap", token, "").Code)
	webToken := a.signSession(newSessionID(), time.Now().Add(time.Hour))
	require.Equal(t, 401, mobileRequest(h, "GET", "/mobile/v1/bootstrap", webToken, "").Code)
	require.Equal(t, 200, mobileRequest(h, "DELETE", "/mobile/v1/session", token, "").Code)
	require.Equal(t, 401, mobileRequest(h, "GET", "/mobile/v1/bootstrap", token, "").Code)
	// Public web never exposes native login, including when an authenticated
	// browser guesses its URL.
	r := httptest.NewRequest("POST", "/mobile/v1/session", bytes.NewBufferString(`{"password":"s3cret"}`))
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: webToken})
	w := httptest.NewRecorder()
	a.NewRouter().ServeHTTP(w, r)
	require.Equal(t, 404, w.Code)
}

func TestMobileAppendRetryAndConflict(t *testing.T) {
	a := testApp(t)
	core := &mobileCore{fakeCore: newFakeCore(), seen: map[string]string{}}
	a.Core = core
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	h := a.NewMobileRouter()
	const id = "515a14ac-5cba-4b4c-a540-71cd0cd12345"
	body := `{"id":"` + id + `","date":"05-Nov-2026","path":"","text":"Текст & <заметка>"}`
	// Simulate lost response: submit exactly the same persisted payload twice.
	for range 2 {
		w := mobileRequest(h, "POST", "/mobile/v1/append", token, body)
		require.Equal(t, 200, w.Code)
		require.JSONEq(t, `{"id":"`+id+`","stored":true}`, w.Body.String())
	}
	require.Equal(t, 1, core.appends)
	w := mobileRequest(h, "POST", "/mobile/v1/append", token, strings.Replace(body, "Текст", "Changed", 1))
	require.Equal(t, 409, w.Code)
	require.NotContains(t, w.Body.String(), "private content")
	for _, invalid := range []string{`{}`, strings.Replace(body, id, "bad", 1), strings.Replace(body, `"path":""`, `"path":"other.md"`, 1), body + `{}`, strings.Replace(body, `"text":`, `"unknown":1,"text":`, 1), `{"text":"` + strings.Repeat("x", 300000) + `"}`} {
		require.Equal(t, 400, mobileRequest(h, "POST", "/mobile/v1/append", token, invalid).Code)
	}
	require.Equal(t, 1, core.appends)
}

func TestMobileAppendFailsClosedWithoutIdempotentCore(t *testing.T) {
	a := testApp(t)
	a.Core = newFakeCore()
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	w := mobileRequest(a.NewMobileRouter(), "POST", "/mobile/v1/append", token, `{"id":"515a14ac-5cba-4b4c-a540-71cd0cd12345","date":"05-Nov-2026","text":"keep me"}`)
	require.Equal(t, 503, w.Code)
}

func TestMobileLoginRateLimitAndCrossOrigin(t *testing.T) {
	a := testApp(t)
	h := a.NewMobileRouter()
	for range loginMaxFailures {
		require.Equal(t, 401, mobileRequest(h, "POST", "/mobile/v1/session", "", `{"password":"bad"}`).Code)
	}
	require.Equal(t, 429, mobileRequest(h, "POST", "/mobile/v1/session", "", `{"password":"s3cret"}`).Code)
	r := httptest.NewRequest("POST", "/mobile/v1/append", nil)
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code)
}

func TestMobileTaskRejectsStaleText(t *testing.T) {
	a := testApp(t)
	a.Core = newFakeCore()
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	w := mobileRequest(a.NewMobileRouter(), "POST", "/mobile/v1/task", token, `{"date":"05-Nov-2026","index":0,"state":2,"text":"old task"}`)
	require.Equal(t, 409, w.Code)
}
