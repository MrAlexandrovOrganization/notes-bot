package webapp

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	pb "notes-bot/proto/notes"
)

func TestWebMarkdownNotePages(t *testing.T) {
	const date = "09-Nov-2025"
	const folderNote = "Планы & дела/Заметка.md"
	const foundNote = "Архив/Заметка.md"
	content := strings.Join([]string{
		"# Заголовок заметки",
		"",
		"- первый пункт",
		"- второй пункт",
		"",
		"````md",
		"## Дневник",
		"```",
		"- [ ] тестовая задача",
		"<literal>",
		"````",
		"",
		"![Снимок](<../Новые фото/Снимок%20(1).png> \"Заголовок\")",
		"![[План & схема%20.png|300]]",
	}, "\n")

	core := newFakeCore()
	core.notes[date] = "## Дневник\n" + content + "\n\n## Задачи\n- [ ] тестовая задача"
	core.notes[folderNote] = content
	core.tasks[date] = []*clients.Task{{Index: 0, Text: "тестовая задача"}}
	a := newDayTestApp(core)
	a.Search = &fakeSearch{note: &clients.SearchNote{ID: 7, Name: "Заметка", Relpath: foundNote, Content: content}}
	pages := http.NewServeMux()
	a.registerDayRoutes(pages)
	a.registerBrowseRoutes(pages)
	a.registerSearchRoutes(pages)

	for _, tc := range []struct {
		request string
		note    string
	}{
		{"/day?date=" + date, "Daily/" + date + ".md"},
		{"/browse/file?path=" + url.QueryEscape(folderNote), folderNote},
		{"/search/note/7", foundNote},
	} {
		t.Run(tc.note, func(t *testing.T) {
			rec := httptest.NewRecorder()
			pages.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.request, nil))
			require.Equal(t, http.StatusOK, rec.Code)
			body := rec.Body.String()
			assert.Contains(t, body, `class="notes-copy notes-markdown"`)
			assert.Contains(t, body, "<h1>Заголовок заметки</h1>")
			assert.Contains(t, body, "<li>первый пункт</li>")
			assert.Contains(t, body, "<li>второй пункт</li>")
			assert.Contains(t, body, "<pre><code class=\"language-md\">## Дневник\n```\n- [ ] тестовая задача\n&lt;literal&gt;")
			assert.Contains(t, body, `alt="Снимок" title="Заголовок"`)
			assert.Contains(t, body, `alt="План &amp; схема%20.png"`)
			for _, ref := range []string{"../Новые фото/Снимок (1).png", "План & схема%20.png"} {
				query := url.Values{"note": {tc.note}, "reference": {ref}}
				assert.Contains(t, body, `src="`+html.EscapeString(views.NoteImagePath+"?"+query.Encode())+`"`)
			}
		})
	}
}

type webMarkdownTestCore struct {
	*fakeCore
	calls     int
	note, ref string
	response  *pb.ReadNoteImageResponse
	err       error
}

func (c *webMarkdownTestCore) ReadNoteImage(_ context.Context, note, ref string) (*pb.ReadNoteImageResponse, error) {
	c.calls++
	c.note, c.ref = note, ref
	return c.response, c.err
}

func TestWebMarkdownImageRequiresSessionAndUsesCore(t *testing.T) {
	const note = "Папка/Заметка.md"
	const ref = "Фото + 1.jpg"
	image := &pb.ReadNoteImageResponse{MediaType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 0xd9}}
	core := &webMarkdownTestCore{fakeCore: newFakeCore(), response: image}
	a := newDayTestApp(core.fakeCore)
	a.Core = core
	mux := http.NewServeMux()
	a.registerWebMarkdownRoutes(mux)
	handler := a.requireAuth(mux)
	query := url.Values{"note": {note}, "reference": {ref}}
	target := views.NoteImagePath + "?" + query.Encode()
	session := a.signSession("fixture", time.Now().Add(time.Hour))

	request := func(target, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, cookie := range []string{"", "forged", a.signSession("expired", time.Now().Add(-time.Hour))} {
		rec := request(target, cookie)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/login", rec.Header().Get("Location"))
		assert.Zero(t, core.calls)
	}
	rec := request(target, session)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, core.calls)
	assert.Equal(t, note, core.note)
	assert.Equal(t, ref, core.ref)
	assert.Equal(t, image.GetData(), rec.Body.Bytes())
	assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))

	// An empty reference must not turn a web preview into direct vault file access.
	rec = request(views.NoteImagePath+"?note=photo.jpg", session)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, 1, core.calls)

	core.err = status.Error(codes.NotFound, "/private/fixture/note.md")
	rec = request(target, session)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, 2, core.calls)
	assert.NotContains(t, rec.Body.String(), "/private/fixture")

	core.err = nil
	core.response = &pb.ReadNoteImageResponse{MediaType: "text/html", Data: []byte("<script></script>")}
	rec = request(target, session)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.NotContains(t, rec.Body.String(), "<script>")
}
