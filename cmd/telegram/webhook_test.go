package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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

func TestTelegramWebhookHandler_CancelledRequestDoesNotBlock(t *testing.T) {
	updates := make(chan tgbotapi.Update)
	handler := telegramWebhookHandler("expected-secret", updates)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"update_id":1}`))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "expected-secret")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	resp := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(resp, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("webhook handler blocked after request cancellation")
	}
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}

func TestTelegramTracingTransportRedactsToken(t *testing.T) {
	const token = "123456:secret"
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		receivedPath = req.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	endpoint, err := url.Parse(server.URL + "/bot" + token + "/getUpdates?offset=42")
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), nil)
	require.NoError(t, err)
	client := &http.Client{Transport: telegramTracingTransport{base: http.DefaultTransport, botToken: token}}
	response, err := client.Do(request)
	require.NoError(t, err)
	response.Body.Close()

	assert.Equal(t, "/bot"+token+"/getUpdates", receivedPath)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attributes := spans[0].Attributes()
	assert.Contains(t, attributes, attribute.String("url.full", server.URL+"/botREDACTED/getUpdates"))
	for _, attr := range attributes {
		assert.NotContains(t, attr.Value.AsString(), token)
	}
}
