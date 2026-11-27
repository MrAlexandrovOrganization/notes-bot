package webapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"notes-bot/internal/mobilevoice"
)

func TestWebVoiceCookieOriginFormatsAndReplay(t *testing.T) {
	const id = "1682698c-07c7-4b07-ae93-0cb7c130e5cd"
	const url = "/voice/" + id
	for _, sample := range []struct {
		name, contentType, audio string
		date, path, storedDate   string
	}{
		{"Safari", "audio/mp4; codecs=mp4a.40.2", "\x00\x00\x00\x1cftypM4A \x00\x00\x00\x00", "2026-11-27", "", "27-Nov-2026"},
		{"Chromium", "audio/webm; codecs=opus", "\x1a\x45\xdf\xa3\x9f\x42\x82\x84webm", "", "Папка/Заметка.md", ""},
		{"Ogg", "application/octet-stream", "OggS\x00\x02synthetic", "", "Synthetic.md", ""},
		{"WAV", "application/octet-stream", "RIFF\x1c\x00\x00\x00WAVEfmt ", "", "Synthetic.md", ""},
	} {
		t.Run(sample.name, func(t *testing.T) {
			a := testApp(t)
			fixture := &voiceFixtureStore{}
			a.Voice = fixture
			protected := http.NewServeMux()
			a.registerWebVoiceRoutes(protected)
			handler := a.requireAuth(protected)
			session := a.signSession(newSessionID(), time.Now().Add(time.Hour))

			call := func(method, auth, origin, date, path, contentType, audio string) *httptest.ResponseRecorder {
				t.Helper()
				var body bytes.Buffer
				var multipartType string
				if method == http.MethodPut {
					form := multipart.NewWriter(&body)
					require.NoError(t, form.WriteField("date", date))
					require.NoError(t, form.WriteField("path", path))
					part, err := form.CreatePart(textproto.MIMEHeader{
						"Content-Disposition": {`form-data; name="audio"; filename="synthetic-audio"`},
						"Content-Type":        {contentType},
					})
					require.NoError(t, err)
					_, err = part.Write([]byte(audio))
					require.NoError(t, err)
					require.NoError(t, form.Close())
					multipartType = form.FormDataContentType()
				}
				req := httptest.NewRequest(method, "https://notes.example"+url, &body)
				req.Header.Set("Origin", origin)
				req.Header.Set("Content-Type", multipartType)
				if auth == "cookie" {
					req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
				} else if auth == "bearer" {
					req.Header.Set("Authorization", "Bearer "+session)
				}
				res := httptest.NewRecorder()
				handler.ServeHTTP(res, req)
				return res
			}
			const sameSite = "https://notes.example"
			require.Equal(t, http.StatusSeeOther, call(http.MethodPut, "bearer", sameSite, sample.date, sample.path, sample.contentType, sample.audio).Code)
			require.Equal(t, http.StatusForbidden, call(http.MethodPut, "cookie", "https://elsewhere.example", sample.date, sample.path, sample.contentType, sample.audio).Code)
			require.Equal(t, http.StatusBadRequest, call(http.MethodPut, "cookie", sameSite, "", "../outside.md", sample.contentType, sample.audio).Code)
			require.Equal(t, http.StatusBadRequest, call(http.MethodPut, "cookie", sameSite, sample.date, sample.path, sample.contentType, "not audio").Code)

			out := call(http.MethodPut, "cookie", sameSite, sample.date, sample.path, sample.contentType, sample.audio)
			require.Equal(t, http.StatusAccepted, out.Code)
			require.Equal(t, mobilevoice.Upload{ID: id, Date: sample.storedDate, Path: sample.path, Audio: []byte(sample.audio)}, fixture.received)
			require.Equal(t, http.StatusOK, call(http.MethodPut, "cookie", sameSite, sample.date, sample.path, sample.contentType, sample.audio).Code)

			fixture.status.State = mobilevoice.Saved
			out = call(http.MethodGet, "cookie", sameSite, "", "", "", "")
			require.Equal(t, http.StatusOK, out.Code)
			require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
			var state mobilevoice.Status
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &state))
			require.Equal(t, mobilevoice.Status{ID: id, State: mobilevoice.Saved}, state)
		})
	}
}
