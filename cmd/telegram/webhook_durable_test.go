package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"
)

const syntheticVoiceUpdate = `{"update_id":77,"message":{"message_id":55,"date":1791792000,"chat":{"id":101,"type":"private"},"from":{"id":101,"is_bot":false,"first_name":"Synthetic"},"voice":{"file_id":"synthetic-file","file_unique_id":"synthetic-unique","duration":3}}}`

func durableWebhookRequest(t *testing.T) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(syntheticVoiceUpdate))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", "synthetic-secret")
	return r
}

type persistenceWatchingWriter struct {
	*httptest.ResponseRecorder
	committed *atomic.Bool
	observed  chan bool
}

func (w persistenceWatchingWriter) WriteHeader(code int) {
	w.observed <- w.committed.Load()
	w.ResponseRecorder.WriteHeader(code)
}

func TestDurableWebhookPersistsBeforeAcknowledgement(t *testing.T) {
	// No receiver exists: accidentally forwarding the accepted update to the
	// volatile queue would block, rather than hiding the bug in a buffer.
	updates := make(chan telego.Update)
	entered, commit := make(chan int, 1), make(chan struct{})
	var committed atomic.Bool
	h := telegramWebhookHandler("synthetic-secret", updates, func(ctx context.Context, u *telego.Update) (bool, error) {
		entered <- u.UpdateID
		select {
		case <-commit:
			committed.Store(true)
			return true, nil
		case <-ctx.Done():
			return true, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	r := durableWebhookRequest(t).WithContext(ctx)
	w := persistenceWatchingWriter{httptest.NewRecorder(), &committed, make(chan bool, 1)}
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, r); close(done) }()
	select {
	case id := <-entered:
		require.Equal(t, 77, id)
	case <-ctx.Done():
		t.Fatal("acceptance hook was not called")
	}
	select {
	case <-w.observed:
		t.Fatal("webhook acknowledged before persistence completed")
	default:
	}
	close(commit)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("durably accepted update blocked on the volatile queue")
	}
	require.True(t, <-w.observed, "response must follow the commit")
	require.Equal(t, http.StatusOK, w.Code)
}

func TestDurableWebhookDatabaseFailureReturns503WithoutQueueingOrDropping(t *testing.T) {
	updates := make(chan telego.Update, 1)
	updates <- telego.Update{UpdateID: 900}
	h := telegramWebhookHandler("synthetic-secret", updates, func(context.Context, *telego.Update) (bool, error) {
		return true, errors.New("synthetic database unavailable")
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, durableWebhookRequest(t))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotContains(t, w.Body.String(), "synthetic database unavailable")
	require.Len(t, updates, 1, "existing queued update must survive the failed acceptance")
	require.Equal(t, 900, (<-updates).UpdateID)
}

func TestDurableWebhookDuplicateIsAcknowledgedWithoutVolatileDelivery(t *testing.T) {
	updates := make(chan telego.Update, 2)
	accepted := map[int]bool{}
	writes, calls := 0, 0
	h := telegramWebhookHandler("synthetic-secret", updates, func(_ context.Context, u *telego.Update) (bool, error) {
		calls++
		if !accepted[u.UpdateID] {
			accepted[u.UpdateID] = true
			writes++
		}
		return true, nil
	})
	for range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, durableWebhookRequest(t))
		require.Equal(t, http.StatusOK, w.Code)
	}
	require.Equal(t, 2, calls, "the persistence layer decides whether an update already exists")
	require.Equal(t, 1, writes)
	require.Empty(t, updates)
}

func TestDurableWebhookUnclaimedUpdateKeepsNormalQueueDelivery(t *testing.T) {
	updates := make(chan telego.Update, 1)
	h := telegramWebhookHandler("synthetic-secret", updates, func(context.Context, *telego.Update) (bool, error) {
		return false, nil
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, durableWebhookRequest(t))
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, updates, 1)
	require.Equal(t, 77, (<-updates).UpdateID)
}
