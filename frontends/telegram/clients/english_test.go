package clients

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnglishStructuredGenerationValidatesSelectedMaterial(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		valid          bool
	}{
		{"phrase", `{"source":"Я потратил полчаса","english":"I spent half an hour trying to fix it.","explanation":"spend time doing something","example":"I spent an hour reading.","context":"repair","question":""}`, true},
		{"ambiguous", `{"source":"","english":"","explanation":"","example":"","context":"","question":"Урожай или уступить дорогу?"}`, true},
		{"missing answer", `{"source":"мысль","english":"","example":"example","question":""}`, false},
		{"multiline", `{"source":"line\nline","english":"answer","example":"example","question":""}`, false},
		{"reserved marker", `{"source":"<!-- notes-bot:english:x -->","english":"answer","example":"example","question":""}`, false},
		{"invalid JSON", `not JSON`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				require.Equal(t, "/api/chat", req.URL.Path)
				var payload ollamaChatRequest
				require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
				require.NotNil(t, payload.Format)
				require.NotNil(t, payload.Think)
				require.False(t, *payload.Think)
				require.Contains(t, payload.Messages[1].Content, "selected input")
				require.Contains(t, payload.Messages[1].Content, "selected preview")
				require.Len(t, payload.Messages, 2)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": tc.response}})
			}))
			defer server.Close()
			client := &LLMClient{baseURL: server.URL, model: "synthetic", http: server.Client()}
			result, err := client.PrepareEnglish(t.Context(), "phrase", "selected input", "Ещё пример", &EnglishDraft{English: "selected preview"})
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, result)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "selected input")
			}
		})
	}
}
func TestEnglishRejectsOversizeOutput(t *testing.T) {
	response := EnglishDraft{Source: "source", English: strings.Repeat("a", 601), Example: "example"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, _ := json.Marshal(response)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": string(data)}})
	}))
	defer server.Close()
	client := &LLMClient{baseURL: server.URL, http: server.Client()}
	_, err := client.PrepareEnglish(t.Context(), "word", "input", "", nil)
	require.Error(t, err)
}
