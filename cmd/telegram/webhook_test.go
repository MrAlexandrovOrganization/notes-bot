package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramWebhookHandler(t *testing.T) {
	updates := make(chan tgbotapi.Update, 1)
	handler := telegramWebhookHandler("expected-secret", updates)

	t.Run("rejects missing secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"update_id":1}`))
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assert.Empty(t, updates)
	})

	t.Run("accepts valid secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"update_id":42}`))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "expected-secret")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)
		require.Len(t, updates, 1)
		assert.Equal(t, 42, (<-updates).UpdateID)
	})
}
