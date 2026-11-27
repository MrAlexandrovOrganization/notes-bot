package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/internal/english"
	pb "notes-bot/proto/english/v1"
	notes "notes-bot/proto/notes"
)

type webEnglishTutor struct {
	clients.SearchService
	generate func(kind, input, instruction string) (*clients.EnglishDraft, error)
}

func (t webEnglishTutor) PrepareEnglish(_ context.Context, kind, input, instruction string, _ *clients.EnglishDraft) (*clients.EnglishDraft, error) {
	return t.generate(kind, input, instruction)
}

// Run saves against the real dictionary logic, with only its Markdown boundary in memory.
type webEnglishDictionary struct {
	pb.EnglishServiceClient
	content  string
	revision int
	list     *pb.ListCardsRequest
	dropNext bool
}

func (d *webEnglishDictionary) server() *english.Server {
	return &english.Server{Dictionary: english.Dictionary{Notes: d}}
}

func (d *webEnglishDictionary) ListCards(_ context.Context, req *pb.ListCardsRequest, _ ...grpc.CallOption) (*pb.ListCardsResponse, error) {
	d.list = req
	return &pb.ListCardsResponse{Dictionaries: []*pb.DictionaryInfo{
		{Id: "general", Title: "Основной"}, {Id: "movie", Title: "Фильм"},
	}}, nil
}

func (d *webEnglishDictionary) SaveCard(ctx context.Context, req *pb.SaveCardRequest, _ ...grpc.CallOption) (*pb.SaveCardResponse, error) {
	resp, err := d.server().SaveCard(ctx, req)
	if err == nil && d.dropNext {
		d.dropNext = false
		return nil, status.Error(codes.Unavailable, "lost reply")
	}
	return resp, err
}

func (d *webEnglishDictionary) ListDirectory(context.Context, *notes.ListDirectoryRequest, ...grpc.CallOption) (*notes.ListDirectoryResponse, error) {
	return nil, status.Error(codes.NotFound, "no themed files")
}

func (d *webEnglishDictionary) ReadMarkdown(_ context.Context, req *notes.ReadMarkdownRequest, _ ...grpc.CallOption) (*notes.ReadMarkdownResponse, error) {
	if req.GetRelpath() != "Tags/English/words.md" {
		return &notes.ReadMarkdownResponse{Revision: "missing"}, nil
	}
	return &notes.ReadMarkdownResponse{Content: d.content, Revision: strconv.Itoa(d.revision)}, nil
}

func (d *webEnglishDictionary) ReplaceMarkdown(_ context.Context, req *notes.ReplaceMarkdownRequest, _ ...grpc.CallOption) (*notes.ReplaceMarkdownResponse, error) {
	if req.GetExpectedRevision() != strconv.Itoa(d.revision) {
		return nil, status.Error(codes.Aborted, "revision mismatch")
	}
	d.content = req.GetContent()
	d.revision++
	return &notes.ReplaceMarkdownResponse{}, nil
}

func TestWebEnglishPrepareThenConfirmWithStableRetry(t *testing.T) {
	a := testApp(t)
	dictionary := &webEnglishDictionary{}
	a.English = dictionary
	core := &fakeCore{todayDate: "27-Nov-2026"}
	a.Core = core
	calls := 0
	a.Search = webEnglishTutor{generate: func(kind, input, instruction string) (*clients.EnglishDraft, error) {
		calls++
		require.Equal(t, "word", kind)
		require.Equal(t, "bank", input)
		switch calls {
		case 1:
			return &clients.EnglishDraft{Question: "Речь о здании или о береге?"}, nil
		case 2:
			require.Equal(t, "о береге", instruction)
			return nil, status.Error(codes.Unavailable, "upstream private details")
		default:
			require.Equal(t, "о береге", instruction)
			return &clients.EnglishDraft{English: "bank", Source: "берег", Example: "We sat on the bank."}, nil
		}
	}}
	request := englishBrowserRequests(a)
	const words = "/english/words"
	const dictionaryURL = "/english?q=bank&scope=general&scope=movie&view=dictionary"
	page := request(http.MethodGet, dictionaryURL, nil)
	require.Equal(t, http.StatusOK, page.Code)
	require.Equal(t, "general+movie", dictionary.list.GetDictionaryId())
	require.Contains(t, page.Body.String(), `action="/english/words" method="post"`)
	require.Contains(t, page.Body.String(), "Подготовить карточку")
	operations := regexp.MustCompile(`name="operation" type="hidden" value="([^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	require.Len(t, operations, 2)
	require.NotEqual(t, operations[0][1], operations[1][1])

	form := url.Values{
		"scope": {"movie", "general"}, "q": {"bank"}, "operation": {operations[1][1]}, "input": {" bank "},
	}
	question := request(http.MethodPost, words+"/prepare", form)
	require.Equal(t, http.StatusOK, question.Code)
	require.Contains(t, question.Body.String(), "Речь о здании или о береге?")
	require.NotContains(t, question.Body.String(), words+"/confirm")
	require.Empty(t, dictionary.content)

	form.Set("instruction", "о береге")
	failed := request(http.MethodPost, words+"/prepare", form)
	require.Equal(t, http.StatusServiceUnavailable, failed.Code)
	require.Contains(t, failed.Body.String(), `name="input" value=" bank "`)
	require.Contains(t, failed.Body.String(), `name="instruction" value="о береге"`)
	require.NotContains(t, failed.Body.String(), "upstream private details")
	require.Empty(t, dictionary.content)

	prepared := request(http.MethodPost, words+"/prepare", form)
	require.Equal(t, http.StatusOK, prepared.Code)
	for _, value := range []string{
		`name="english" value="bank"`, `name="source" value="берег"`,
		`name="example" value="We sat on the bank."`, `name="date" value="27-Nov-2026"`,
		"Подтвердить и сохранить",
	} {
		require.Contains(t, prepared.Body.String(), value)
	}
	require.Contains(t, prepared.Body.String(), strings.ReplaceAll(dictionaryURL, "&", "&amp;"))
	require.Empty(t, dictionary.content)

	form.Set("date", core.todayDate)
	form.Set("english", "riverbank")
	form.Set("source", "берег реки")
	form.Set("example", "We walked along the riverbank.")
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, words+"/confirm", form).Code)
	require.Empty(t, dictionary.content)

	form.Del("input")
	form.Del("instruction")
	form.Set("confirm", "1")
	dictionary.dropNext = true
	saveFailed := request(http.MethodPost, words+"/confirm", form)
	require.Equal(t, http.StatusServiceUnavailable, saveFailed.Code)
	require.Contains(t, saveFailed.Body.String(), `name="example" value="We walked along the riverbank."`)
	require.Contains(t, saveFailed.Body.String(), `name="date" value="27-Nov-2026"`)
	core.todayDate = "28-Nov-2026"
	for range 2 {
		saved := request(http.MethodPost, words+"/confirm", form)
		require.Equal(t, http.StatusSeeOther, saved.Code)
		location, err := url.Parse(saved.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, form["scope"], location.Query()["scope"])
		require.Equal(t, "bank", location.Query().Get("q"))
		require.Equal(t, "dictionary", location.Query().Get("view"))
	}
	listed, err := dictionary.server().ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetCards(), 1)
	card := listed.GetCards()[0]
	require.Equal(t, "riverbank", card.GetEnglish())
	require.Equal(t, "берег реки", card.GetSource())
	require.Equal(t, "We walked along the riverbank.", card.GetExample())
	require.Equal(t, "27-Nov-2026", card.GetDate())
	require.Equal(t, 3, calls)
}

func TestWebEnglishRussianPreparationAndManualEntry(t *testing.T) {
	a := testApp(t)
	dictionary := &webEnglishDictionary{}
	a.English = dictionary
	a.Core = &fakeCore{todayDate: "27-Nov-2026"}
	a.Search = webEnglishTutor{generate: func(kind, input, instruction string) (*clients.EnglishDraft, error) {
		require.Equal(t, "phrase", kind)
		require.Equal(t, "берег  реки", input)
		require.Empty(t, instruction)
		return &clients.EnglishDraft{English: "riverbank", Source: "подмена", Example: "We stopped by the riverbank."}, nil
	}}
	request := englishBrowserRequests(a)
	const operation = "12345678-1234-4567-8901-123456789012"
	hidden := request(http.MethodGet, "/english?view=dictionary&hidden=1&scope=movie", nil)
	require.Equal(t, http.StatusOK, hidden.Code)
	require.True(t, dictionary.list.GetHiddenOnly())
	require.Equal(t, "movie", dictionary.list.GetDictionaryId())
	require.NotContains(t, hidden.Body.String(), "Подготовить карточку")

	form := url.Values{"operation": {operation}, "input": {"берег  реки"}}
	prepared := request(http.MethodPost, "/english/words/prepare", form)
	require.Equal(t, http.StatusOK, prepared.Code)
	require.Contains(t, prepared.Body.String(), `name="source" value="берег реки"`)
	require.NotContains(t, prepared.Body.String(), "подмена")
	require.Empty(t, dictionary.content)

	manual := request(http.MethodPost, "/english/words", url.Values{"operation": {operation}, "english": {"pebble"}, "source": {"галька"}})
	require.Equal(t, http.StatusSeeOther, manual.Code)
	listed, err := dictionary.server().ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetCards(), 1)
	require.Equal(t, "pebble", listed.GetCards()[0].GetEnglish())
}

func englishBrowserRequests(a *App) func(method, target string, form url.Values) *httptest.ResponseRecorder {
	router := a.NewRouter()
	cookie := &http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))}
	return func(method, target string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
}
