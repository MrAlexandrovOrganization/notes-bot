package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/config"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (f telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func telegramResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestTelegoClientEndpointsAndWebhook(t *testing.T) {
	const token = "123456:abcdefghijklmnopqrstuvwxyz123456789"
	for _, local := range []string{"", "http://telegram-api.test:8081/"} {
		t.Run(local, func(t *testing.T) {
			origin := strings.TrimSuffix(local, "/")
			if origin == "" {
				origin = "https://api.telegram.org"
			}
			var methods []string
			client := &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, origin, req.URL.Scheme+"://"+req.URL.Host)
				method := strings.TrimPrefix(req.URL.Path, "/bot"+token+"/")
				methods = append(methods, method)
				var params map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&params))
				switch method {
				case "getMe":
					return telegramResponse(`{"ok":true,"result":{"id":123456,"is_bot":true,"first_name":"Test","username":"test_bot"}}`), nil
				case "setWebhook":
					assert.Equal(t, "https://bot.test/webhook", params["url"])
					assert.Equal(t, "synthetic-secret", params["secret_token"])
				case "deleteWebhook":
					assert.NotEqual(t, true, params["drop_pending_updates"])
				default:
					t.Errorf("unexpected method %s", method)
				}
				return telegramResponse(`{"ok":true,"result":true}`), nil
			})}
			bot, err := newTelegramBot(&config.Config{BOTToken: token, LocalAPIURL: local}, client, zap.NewNop())
			require.NoError(t, err)
			me, err := bot.GetMe(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "test_bot", me.Username)
			require.NoError(t, bot.SetWebhook(t.Context(), &telego.SetWebhookParams{URL: "https://bot.test/webhook", SecretToken: "synthetic-secret"}))
			require.NoError(t, bot.DeleteWebhook(t.Context(), &telego.DeleteWebhookParams{}))
			assert.Equal(t, []string{"getMe", "setWebhook", "deleteWebhook"}, methods)
			assert.Equal(t, origin+"/file/bot"+token+"/voice/test.ogg", bot.FileDownloadURL("voice/test.ogg"))
		})
	}
}

func TestTelegoClientCancellationRedactsError(t *testing.T) {
	token := "123:" + strings.Repeat("a", 35)
	ctx, cancel := context.WithCancel(t.Context())
	client := &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		return nil, req.Context().Err()
	})}
	bot, err := newTelegramBot(&config.Config{BOTToken: token}, client, zap.NewNop())
	require.NoError(t, err)
	_, err = bot.GetMe(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), token)
	assert.Contains(t, err.Error(), "REDACTED")
}

func TestRunPollingCancelsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requested := make(chan int, 1)
	client := &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var params telego.GetUpdatesParams
		if err := json.NewDecoder(req.Body).Decode(&params); err != nil {
			return nil, err
		}
		requested <- params.Timeout
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	bot, err := newTelegramBot(&config.Config{BOTToken: "123:" + strings.Repeat("a", 35)}, client, zap.NewNop())
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPolling(ctx, bot, nil, &sync.WaitGroup{}, zap.NewNop())
	}()
	select {
	case timeout := <-requested:
		assert.Equal(t, 60, timeout)
	case <-time.After(3 * time.Second):
		t.Fatal("polling did not request updates")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("polling did not stop on cancellation")
	}
}

func TestClassifyTelegoUpdates(t *testing.T) {
	for _, tt := range []struct {
		name, payload, kind, command string
	}{
		{"command", `{"message":{"from":{"id":42},"text":"/start@test_bot arg","entities":[{"type":"bot_command","offset":0,"length":15}]}}`, "command", "start"},
		{"plain slash text", `{"message":{"from":{"id":42},"text":"/start"}}`, "text", ""},
		{"voice", `{"message":{"from":{"id":42},"voice":{"file_id":"voice"}}}`, "voice", ""},
		{"video note", `{"message":{"from":{"id":42},"video_note":{"file_id":"video"}}}`, "voice", ""},
		{"edited location", `{"edited_message":{"from":{"id":42},"location":{"latitude":1,"longitude":2}}}`, "location", ""},
		{"inaccessible callback", `{"callback_query":{"id":"q","from":{"id":42},"data":"menu:back","message":{"message_id":7,"date":0,"chat":{"id":42,"type":"private"}}}}`, "callback", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var update telego.Update
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &update))
			kind, user := classifyUpdate(&update)
			assert.Equal(t, tt.kind, kind)
			assert.Equal(t, int64(42), user)
			assert.Equal(t, tt.command, messageCommand(update.Message))
		})
	}
}

func TestTelegramAPIErrorPreservesCause(t *testing.T) {
	err := telegramAPIError{err: io.EOF, message: "redacted"}
	assert.True(t, errors.Is(err, io.EOF))
}
