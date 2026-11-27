package webapp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"notes-bot/frontends/telegram/clients"
)

func TestWebDraftAcknowledgesOnlySuccessfulWrites(t *testing.T) {
	a := testApp(t)
	a.Core = newFakeCore()
	handler := a.NewRouter()
	session := &http.Cookie{
		Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour)),
	}
	send := func(route string, values url.Values, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, route, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("X-Notes-Draft", "1")
		if authorized {
			r.AddCookie(session)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}

	day := url.Values{"date": {"27-Nov-2026"}, "text": {"Синтетическая запись"}}
	file := url.Values{"relpath": {"Пример.md"}, "text": {"Синтетическая запись"}}
	for _, tc := range []struct {
		route  string
		values url.Values
	}{
		{"/day/append", day},
		{"/day/tasks", day},
		{"/browse/file/append", file},
		{"/search/note/21/append", file},
	} {
		t.Run(tc.route, func(t *testing.T) {
			response := send(tc.route, tc.values, true)
			require.Equal(t, http.StatusNoContent, response.Code)
			require.Equal(t, "saved", response.Header().Get("X-Notes-Draft"))
			require.Empty(t, response.Body.String())
		})
	}

	response := send("/day/append", url.Values{"date": {"27-Nov-2026"}}, true)
	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	require.Empty(t, response.Header().Get("X-Notes-Draft"))

	response = send("/day/append", day, false)
	require.Equal(t, http.StatusSeeOther, response.Code)
	require.Empty(t, response.Header().Get("X-Notes-Draft"))
}

func TestWebDraftRefreshOfSearchNoteUsesCurrentContent(t *testing.T) {
	a := testApp(t)
	core := newFakeCore()
	core.notes["Пример.md"] = "Свежая запись"
	a.Core = core
	a.Search = &fakeSearch{note: &clients.SearchNote{
		ID: 21, Name: "Пример", Relpath: "Пример.md", Content: "Устаревшая копия",
	}}
	r := httptest.NewRequest(http.MethodGet, "/search/note/21", nil)
	r.AddCookie(&http.Cookie{
		Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour)),
	})
	out := httptest.NewRecorder()
	a.NewRouter().ServeHTTP(out, r)
	require.Equal(t, http.StatusOK, out.Code)
	require.Contains(t, out.Body.String(), "Свежая запись")
	require.NotContains(t, out.Body.String(), "Устаревшая копия")
}
