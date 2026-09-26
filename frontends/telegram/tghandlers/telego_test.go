package tghandlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
)

type apiTransportFunc func(*http.Request) (*http.Response, error)

func (f apiTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fakeTelego(t *testing.T, transport apiTransportFunc) *telego.Bot {
	t.Helper()
	bot, err := telego.NewBot("123:"+strings.Repeat("a", 35), telego.WithHTTPClient(&http.Client{Transport: transport}), telego.WithDiscardLogger())
	require.NoError(t, err)
	return bot
}

func apiResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestTelegoSendEditHTMLAndContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "handler-context")
	var methods []string
	bot := fakeTelego(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "handler-context", req.Context().Value(contextKey{}))
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
		var params map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(req.Body).Decode(&params))
		assert.JSONEq(t, `42`, string(params["chat_id"]))
		assert.JSONEq(t, `"HTML"`, string(params["parse_mode"]))
		assert.JSONEq(t, `"<b>&lt;note&gt; &amp; text</b>"`, string(params["text"]))
		method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
		methods = append(methods, method)
		if method == "sendMessage" {
			assert.JSONEq(t, `true`, string(params["disable_notification"]))
			assert.Contains(t, string(params["reply_markup"]), "smart:yes")
		} else {
			assert.JSONEq(t, `7`, string(params["message_id"]))
			assert.NotContains(t, params, "reply_markup")
		}
		return apiResponse(`{"ok":true,"result":{"message_id":7,"date":1,"chat":{"id":42,"type":"private"}}}`), nil
	})
	text := tgfmt.Bold(tgfmt.Escape("<note> & text"))
	kb := tgkeyboards.SmartConfirm()
	require.NoError(t, sendText(ctx, bot, 42, text, &kb, true))
	query := &telego.CallbackQuery{Message: &telego.Message{MessageID: 7, Chat: telego.Chat{ID: 42}}}
	require.NoError(t, replyToCallback(ctx, bot, query, text, nil))
	assert.Equal(t, []string{"sendMessage", "editMessageText"}, methods)
}

func TestTelegoMessageErrors(t *testing.T) {
	t.Run("unchanged edit is successful", func(t *testing.T) {
		bot := fakeTelego(t, func(*http.Request) (*http.Response, error) {
			return apiResponse(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`), nil
		})
		require.NoError(t, editText(t.Context(), bot, 42, 7, tgfmt.Escape("same"), nil))
	})
	t.Run("cancel stops retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		bot := fakeTelego(t, func(*http.Request) (*http.Response, error) {
			calls++
			cancel()
			return nil, io.EOF
		})
		require.ErrorIs(t, sendText(ctx, bot, 42, tgfmt.Escape("text"), nil, true), context.Canceled)
		assert.Equal(t, 1, calls)
	})
	t.Run("processing message failure does not panic or call LLM", func(t *testing.T) {
		bot := fakeTelego(t, func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport unavailable")
		})
		app := &App{Cfg: &config.Config{}, Logger: zap.NewNop()}
		// No LLM or state store: failure must return before either is used.
		app.handleSmartInput(t.Context(), bot, 42, 42, "text")
		app.handleReminderNLInput(t.Context(), bot, 42, 42, "text")
	})
}

func TestTelegoInaccessibleCallbackOnlyAcknowledges(t *testing.T) {
	for _, message := range []telego.MaybeInaccessibleMessage{nil, &telego.InaccessibleMessage{MessageID: 7, Chat: telego.Chat{ID: 42}}} {
		ack := make(chan string, 1)
		bot := fakeTelego(t, func(req *http.Request) (*http.Response, error) {
			ack <- req.URL.Path
			return apiResponse(`{"ok":true,"result":true}`), nil
		})
		app := &App{Cfg: &config.Config{RootID: 42}, Logger: zap.NewNop()}
		app.HandleCallback(t.Context(), bot, &telego.Update{CallbackQuery: &telego.CallbackQuery{ID: "query", From: telego.User{ID: 42}, Data: "menu:back", Message: message}})
		select {
		case path := <-ack:
			assert.True(t, strings.HasSuffix(path, "/answerCallbackQuery"))
		case <-time.After(3 * time.Second):
			t.Fatal("callback was not acknowledged")
		}
	}
}
