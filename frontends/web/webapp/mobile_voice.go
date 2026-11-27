package webapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"notes-bot/internal/mobilevoice"
)

type mobileVoiceStore interface {
	Enqueue(context.Context, mobilevoice.Upload) (mobilevoice.Status, bool, error)
	Get(context.Context, string) (mobilevoice.Status, error)
}

var errInvalidVoice = errors.New("invalid voice upload")

func (a *App) registerMobileVoice(mux *http.ServeMux) {
	a.registerVoice(mux, "/mobile/v1/voice/{id}", false)
}

func (a *App) registerVoice(mux *http.ServeMux, pattern string, web bool) {
	// Bound in-flight audio buffers even if several clients retry after a VPN outage.
	uploadSlot := make(chan struct{}, 1)
	mux.HandleFunc("PUT "+pattern, func(w http.ResponseWriter, r *http.Request) {
		if a.Voice == nil {
			mobileError(w, http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		if !mobileOperationID.MatchString(id) {
			mobileError(w, http.StatusBadRequest)
			return
		}
		select {
		case uploadSlot <- struct{}{}:
			defer func() { <-uploadSlot }()
		default:
			w.Header().Set("Retry-After", "5")
			mobileError(w, http.StatusTooManyRequests)
			return
		}
		a.uploadVoice(w, r, id, web)
	})
	mux.HandleFunc("GET "+pattern, a.voiceStatus)
}

func (a *App) uploadVoice(w http.ResponseWriter, r *http.Request, id string, web bool) {
	r.Body = http.MaxBytesReader(w, r.Body, mobilevoice.MaxAudioBytes+(16<<10))
	upload, err := readVoice(r, id, web)
	if err != nil {
		var tooLarge *http.MaxBytesError
		var networkErr net.Error
		switch {
		case errors.As(err, &tooLarge):
			mobileError(w, http.StatusRequestEntityTooLarge)
		case errors.As(err, &networkErr) && networkErr.Timeout():
			mobileError(w, http.StatusServiceUnavailable)
		default:
			mobileError(w, http.StatusBadRequest)
		}
		return
	}
	result, created, err := a.Voice.Enqueue(r.Context(), upload)
	switch {
	case errors.Is(err, mobilevoice.ErrConflict):
		mobileError(w, http.StatusConflict)
	case errors.Is(err, mobilevoice.ErrQueueFull):
		w.Header().Set("Retry-After", "30")
		mobileError(w, http.StatusTooManyRequests)
	case err != nil:
		mobileError(w, http.StatusServiceUnavailable)
	case created:
		mobileJSON(w, http.StatusAccepted, result)
	default:
		mobileJSON(w, http.StatusOK, result)
	}
}

func (a *App) voiceStatus(w http.ResponseWriter, r *http.Request) {
	if a.Voice == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if !mobileOperationID.MatchString(id) {
		mobileError(w, http.StatusBadRequest)
		return
	}
	result, err := a.Voice.Get(r.Context(), id)
	switch {
	case errors.Is(err, mobilevoice.ErrNotFound):
		mobileError(w, http.StatusNotFound)
	case err != nil:
		mobileError(w, http.StatusServiceUnavailable)
	default:
		mobileJSON(w, http.StatusOK, result)
	}
}

func readVoice(r *http.Request, id string, web bool) (mobilevoice.Upload, error) {
	var upload = mobilevoice.Upload{ID: id}
	reader, err := r.MultipartReader()
	if err != nil {
		return upload, err
	}
	seen := make(map[string]bool, 3)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return upload, err
		}
		name := part.FormName()
		if seen[name] {
			return upload, errInvalidVoice
		}
		seen[name] = true
		switch name {
		case "audio":
			if !web {
				mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
				if err != nil || mediaType != "audio/mp4" {
					return upload, errInvalidVoice
				}
			}
			data, err := readVoicePart(part, mobilevoice.MaxAudioBytes)
			if err != nil {
				return upload, err
			}
			if !validVoiceAudio(data, web) {
				return upload, errInvalidVoice
			}
			upload.Audio = data
		case "date", "path":
			field, err := readVoicePart(part, 4096)
			if err != nil {
				return upload, err
			}
			if name == "date" {
				upload.Date = string(field)
			} else {
				upload.Path = string(field)
			}
		default:
			return upload, errInvalidVoice
		}
	}
	if len(upload.Audio) == 0 || (upload.Date == "") == (upload.Path == "") {
		return upload, errInvalidVoice
	}
	if upload.Date != "" {
		upload.Date = voiceNoteDate(upload.Date)
		if upload.Date == "" {
			return upload, errInvalidVoice
		}
	}
	if upload.Path != "" && (!utf8.ValidString(upload.Path) ||
		path.IsAbs(upload.Path) || path.Clean(upload.Path) != upload.Path ||
		strings.HasPrefix(upload.Path, "../") || path.Ext(upload.Path) != ".md" ||
		strings.ContainsRune(upload.Path, '\\') || strings.ContainsFunc(upload.Path, unicode.IsControl)) {
		return upload, errInvalidVoice
	}
	return upload, nil
}

func validVoiceAudio(data []byte, web bool) bool {
	if web {
		return mobilevoice.WebAudioFormat(data) != ""
	}
	return len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp"))
}

func readVoicePart(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if int64(len(b)) > limit {
		return nil, &http.MaxBytesError{Limit: limit}
	}
	return b, err
}

func voiceNoteDate(date string) string {
	for _, layout := range []string{time.DateOnly, "02-Jan-2006"} {
		t, err := time.Parse(layout, date)
		if err == nil && t.Year() > 0 && t.Format(layout) == date {
			return t.Format("02-Jan-2006")
		}
	}
	return ""
}
