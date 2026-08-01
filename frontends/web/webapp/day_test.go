package webapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/config"
)

func newDayTestApp(core *fakeCore) *App {
	return &App{
		Cfg:    &config.Config{WebPassword: "s3cret", WebSessionSecret: "test-signing-secret"},
		Core:   core,
		Logger: zap.NewNop(),
	}
}

func TestHandleDayView_UsesTodayWhenNoDateGiven(t *testing.T) {
	core := newFakeCore()
	core.todayDate = "01-Jan-2026"
	core.notes["01-Jan-2026"] = "hello"
	core.tasks["01-Jan-2026"] = []*clients.Task{{Text: "buy milk", Completed: false, Index: 0}}

	a := newDayTestApp(core)
	req := httptest.NewRequest(http.MethodGet, "/day", nil)
	rec := httptest.NewRecorder()

	a.handleDayView(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "01-Jan-2026", core.ensuredNote)
	assert.Contains(t, rec.Body.String(), "hello")
	assert.Contains(t, rec.Body.String(), "buy milk")
}

func TestHandleDayView_UsesDateQueryParam(t *testing.T) {
	core := newFakeCore()
	core.notes["09-Nov-2025"] = "specific day"

	a := newDayTestApp(core)
	req := httptest.NewRequest(http.MethodGet, "/day?date=09-Nov-2025", nil)
	rec := httptest.NewRecorder()

	a.handleDayView(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "09-Nov-2025", core.ensuredNote)
	assert.Contains(t, rec.Body.String(), "specific day")
}

func TestHandleUpdateRating_RejectsOutOfRange(t *testing.T) {
	core := newFakeCore()
	a := newDayTestApp(core)

	req := httptest.NewRequest(http.MethodPost, "/day/rating", nil)
	req.PostForm = map[string][]string{"date": {"01-Jan-2026"}, "rating": {"11"}}
	rec := httptest.NewRecorder()

	a.handleUpdateRating(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Zero(t, core.updatedRating)
}

func TestHandleUpdateRating_AcceptsValidRating(t *testing.T) {
	core := newFakeCore()
	a := newDayTestApp(core)

	req := httptest.NewRequest(http.MethodPost, "/day/rating", nil)
	req.PostForm = map[string][]string{"date": {"01-Jan-2026"}, "rating": {"8"}}
	rec := httptest.NewRecorder()

	a.handleUpdateRating(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 8, core.updatedRating)
}

func TestHandleAddTask_RejectsEmptyText(t *testing.T) {
	core := newFakeCore()
	a := newDayTestApp(core)

	req := httptest.NewRequest(http.MethodPost, "/day/tasks", nil)
	req.PostForm = map[string][]string{"date": {"01-Jan-2026"}, "text": {""}}
	rec := httptest.NewRecorder()

	a.handleAddTask(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Empty(t, core.addedTaskText)
}
