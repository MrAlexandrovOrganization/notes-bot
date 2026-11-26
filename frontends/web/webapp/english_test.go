package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "notes-bot/proto/english/v1"
)

type webEnglishFixture struct {
	*mobileEnglishClient
}

func (f webEnglishFixture) ListDictionaries(context.Context, *pb.ListDictionariesRequest, ...grpc.CallOption) (*pb.ListDictionariesResponse, error) {
	return &pb.ListDictionariesResponse{Dictionaries: []*pb.DictionaryInfo{{Id: "movie", Title: "Фильмы"}}}, nil
}

func TestWebEnglishReviewUsesSameQueue(t *testing.T) {
	app := testApp(t)
	token := strings.Repeat("a", 64)
	fake := &mobileEnglishClient{current: &pb.ReviewPosition{
		Card:  &pb.Card{English: "cliff", Source: "утёс"},
		Token: token, Round: 2, Position: 1, RoundSize: 9, PromptLanguage: "ru",
	}}
	app.English = webEnglishFixture{fake}
	router := app.NewRouter()
	cookie := &http.Cookie{Name: sessionCookieName, Value: app.signSession(newSessionID(), time.Now().Add(time.Hour))}
	req := httptest.NewRequest("GET", "/english?scope=movie&scope=general", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "утёс")
	require.Contains(t, w.Body.String(), "Фильмы")
	require.Contains(t, w.Body.String(), "Показать ответ")
	require.Contains(t, w.Body.String(), "Разделы Notes")
	require.Equal(t, "general+movie", fake.review.GetDictionaryId())

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
}
