package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocationClientForwardsRawUpdateWithoutNormalizingIt(t *testing.T) {
	raw := json.RawMessage(`{"update_id":4,"message_date":1234,"edit_date":2345,"location":{"latitude":10,"longitude":20,"horizontal_accuracy":7,"heading":180}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/telegram/measurements" || r.Method != http.MethodPost {
			t.Error("wrong ingestion route")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != string(raw) {
			t.Error("frontend rewrote original measurement")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"synthetic","date":"2026-01-01","recorded_at":"2026-01-01T12:00:00Z"}`))
	}))
	defer server.Close()
	client := &LocationClient{baseURL: server.URL, http: server.Client()}
	saved, err := client.Save(context.Background(), &SaveLocationInput{TelegramRaw: raw})
	if err != nil || saved.Date != "2026-01-01" {
		t.Fatal("server date was not returned", err)
	}
}
