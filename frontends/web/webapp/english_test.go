package webapp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/english/v1"
)

func TestWebEnglishReviewUsesSameQueue(t *testing.T) {
	app := testApp(t)
	token := strings.Repeat("a", 64)
	next := strings.Repeat("b", 64)
	fake := &mobileEnglishClient{
		current: &pb.ReviewPosition{
			Card:  &pb.Card{English: "… cliff", Source: "утёс"},
			Token: token, Round: 2, Position: 1, RoundSize: 2, PromptLanguage: "ru",
		},
		next: &pb.ReviewPosition{
			Card:  &pb.Card{English: "dream", Source: "мечта </script>"},
			Token: next, Round: 2, Position: 2, RoundSize: 2, PromptLanguage: "en",
		},
	}
	app.English = fake
	router := app.NewRouter()
	cookie := &http.Cookie{Name: sessionCookieName, Value: app.signSession(newSessionID(), time.Now().Add(time.Hour))}
	req := httptest.NewRequest("GET", "/english?scope=movie&scope=general", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "утёс")
	require.Contains(t, w.Body.String(), "Фильм")
	require.Contains(t, w.Body.String(), "Показать ответ")
	require.Contains(t, w.Body.String(), "Подсказка")
	require.Contains(t, w.Body.String(), `data-review-hint-letter>c</span>`)
	require.Contains(t, w.Body.String(), "Разделы Notes")
	require.Contains(t, w.Body.String(), `"token":"`+next+`"`)
	require.Contains(t, w.Body.String(), `мечта \u003c/script\u003e`)
	require.Equal(t, "general+movie", fake.review.GetDictionaryId())
	require.True(t, fake.review.GetIncludeRemaining())

	req = httptest.NewRequest("GET", "/english/round?scope=general&scope=movie", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusSeeOther, w.Code)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"token":"`+next+`"`)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	form := url.Values{"scope": {"movie", "general"}, "token": {token}, "action": {"easy"}}
	req = httptest.NewRequest("POST", "/english/grade", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Len(t, fake.calls, 1)
	require.Equal(t, "general+movie", fake.calls[0].GetDictionaryId())
	require.Equal(t, pb.ReviewAction_REVIEW_ACTION_EASY, fake.calls[0].GetAction())

	grade := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/english/grade", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.AddCookie(cookie)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result
	}
	require.Equal(t, http.StatusNoContent, grade().Code)
	fake.current = fake.next
	conflict := grade()
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.NotContains(t, conflict.Body.String(), "private details")
}
