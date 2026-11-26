package webapp

import (
	"bytes"
	"context"
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

type voiceFixtureStore struct {
	received mobilevoice.Upload
	status   mobilevoice.Status
}

func (s *voiceFixtureStore) Enqueue(_ context.Context, upload mobilevoice.Upload) (mobilevoice.Status, bool, error) {
	if s.status.ID == "" {
		s.received = upload
		s.status = mobilevoice.Status{ID: upload.ID, State: mobilevoice.Queued}
		return s.status, true, nil
	}
	if s.received.Date != upload.Date || s.received.Path != upload.Path || !bytes.Equal(s.received.Audio, upload.Audio) {
		return mobilevoice.Status{}, false, mobilevoice.ErrConflict
	}
	return s.status, false, nil
}

func (s *voiceFixtureStore) Get(_ context.Context, id string) (mobilevoice.Status, error) {
	if s.status.ID == id {
		return s.status, nil
	}
	return mobilevoice.Status{}, mobilevoice.ErrNotFound
}

func TestMobileVoiceAcceptsAuthorizedUploadAndStatus(t *testing.T) {
	a := testApp(t)
	fixture := &voiceFixtureStore{}
	a.Voice = fixture
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	mobile := a.NewMobileRouter()
	id := "1682698c-07c7-4b07-ae93-0cb7c130e5cd"
	url := "/mobile/v1/voice/" + id
	audio := []byte("\x00\x00\x00\x1cftypM4A \x00\x00\x00\x00")
	put := func(router http.Handler, auth, date, path string, audio []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		require.NoError(t, form.WriteField("date", date))
		require.NoError(t, form.WriteField("path", path))
		part, err := form.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="audio"; filename="voice.m4a"`},
			"Content-Type":        {"audio/mp4"},
		})
		require.NoError(t, err)
		_, err = part.Write(audio)
		require.NoError(t, err)
		require.NoError(t, form.Close())
		r := httptest.NewRequest(http.MethodPut, url, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, put(mobile, "", "2026-11-27", "", audio).Code)
	require.Equal(t, http.StatusSeeOther, put(a.NewRouter(), token, "2026-11-27", "", audio).Code)
	require.Equal(t, http.StatusBadRequest, put(mobile, token, "2026-11-31", "", audio).Code)
	require.Equal(t, http.StatusBadRequest, put(mobile, token, "2026-11-27", "valid.md", audio).Code)
	require.Equal(t, http.StatusBadRequest, put(mobile, token, "", "../outside.md", audio).Code)
	require.Equal(t, http.StatusBadRequest, put(mobile, token, "", "valid.md", []byte("invalid")).Code)
	require.Equal(t, http.StatusRequestEntityTooLarge, put(mobile, token, "", "valid.md", make([]byte, mobilevoice.MaxAudioBytes+1)).Code)

	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mobile.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusNotFound, get().Code)

	out := put(mobile, token, "2026-11-27", "", audio)
	require.Equal(t, http.StatusAccepted, out.Code)
	var job mobilevoice.Status
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &job))
	require.Equal(t, mobilevoice.Status{ID: id, State: mobilevoice.Queued}, job)
	require.Equal(t, mobilevoice.Upload{ID: id, Date: "27-Nov-2026", Audio: audio}, fixture.received)
	require.Equal(t, http.StatusOK, put(mobile, token, "27-Nov-2026", "", audio).Code)
	require.Equal(t, http.StatusConflict, put(mobile, token, "", "other.md", audio).Code)

	fixture.status.State = mobilevoice.Saved
	out = get()
	require.Equal(t, http.StatusOK, out.Code)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"id":"`+id+`","status":"saved"}`, out.Body.String())
}
