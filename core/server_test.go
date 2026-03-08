package core

import (
	"context"
	"errors"
	"testing"

	"notes_bot/core/features"
	pb "notes_bot/proto/notes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- Моки с function-полями ---
// Если поле nil — возвращается безопасное значение по умолчанию.

type mockCalendarStore struct {
	todayDateFn        func() string
	getExistingDatesFn func() ([]string, error)
}

func (m *mockCalendarStore) TodayDate() string {
	if m.todayDateFn != nil {
		return m.todayDateFn()
	}
	return "01-Mar-2026"
}

func (m *mockCalendarStore) GetExistingDates() ([]string, error) {
	if m.getExistingDatesFn != nil {
		return m.getExistingDatesFn()
	}
	return nil, nil
}

type mockNoteStore struct {
	readNoteFn    func(date string) (string, error)
	ensureNoteFn  func(date string) error
	appendToNoteFn func(date, text string) error
}

func (m *mockNoteStore) ReadNote(date string) (string, error) {
	if m.readNoteFn != nil {
		return m.readNoteFn(date)
	}
	return "", nil
}

func (m *mockNoteStore) EnsureNote(date string) error {
	if m.ensureNoteFn != nil {
		return m.ensureNoteFn(date)
	}
	return nil
}

func (m *mockNoteStore) AppendToNote(date, text string) error {
	if m.appendToNoteFn != nil {
		return m.appendToNoteFn(date, text)
	}
	return nil
}

type mockRatingStore struct {
	getRatingFn    func(content string) *int
	updateRatingFn func(date string, rating int) error
}

func (m *mockRatingStore) GetRating(content string) *int {
	if m.getRatingFn != nil {
		return m.getRatingFn(content)
	}
	return nil
}

func (m *mockRatingStore) UpdateRating(date string, rating int) error {
	if m.updateRatingFn != nil {
		return m.updateRatingFn(date, rating)
	}
	return nil
}

type mockTaskStore struct {
	parseTasksFn func(content string) []features.Task
	toggleTaskFn func(date string, index int) error
	addTaskFn    func(date, text string) error
}

func (m *mockTaskStore) ParseTasks(content string) []features.Task {
	if m.parseTasksFn != nil {
		return m.parseTasksFn(content)
	}
	return nil
}

func (m *mockTaskStore) ToggleTask(date string, index int) error {
	if m.toggleTaskFn != nil {
		return m.toggleTaskFn(date, index)
	}
	return nil
}

func (m *mockTaskStore) AddTask(date, text string) error {
	if m.addTaskFn != nil {
		return m.addTaskFn(date, text)
	}
	return nil
}

// newServer создаёт сервер с моками (nil-поля используют дефолтные реализации)
func newServer(calendar CalendarStore, notes NoteStore, ratings RatingStore, tasks TaskStore) *NotesServer {
	s := &NotesServer{}
	if calendar != nil {
		s.calendar = calendar
	} else {
		s.calendar = &mockCalendarStore{}
	}
	if notes != nil {
		s.notes = notes
	} else {
		s.notes = &mockNoteStore{}
	}
	if ratings != nil {
		s.ratings = ratings
	} else {
		s.ratings = &mockRatingStore{}
	}
	if tasks != nil {
		s.tasks = tasks
	} else {
		s.tasks = &mockTaskStore{}
	}
	return s
}

// --- GetTodayDate ---

func TestServer_GetTodayDate_ReturnsDate(t *testing.T) {
	srv := newServer(&mockCalendarStore{
		todayDateFn: func() string { return "08-Mar-2026" },
	}, nil, nil, nil)

	resp, err := srv.GetTodayDate(context.Background(), &pb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "08-Mar-2026", resp.Date)
}

// --- GetExistingDates ---

func TestServer_GetExistingDates_ReturnsDates(t *testing.T) {
	srv := newServer(&mockCalendarStore{
		getExistingDatesFn: func() ([]string, error) {
			return []string{"01-Mar-2026", "02-Mar-2026"}, nil
		},
	}, nil, nil, nil)

	resp, err := srv.GetExistingDates(context.Background(), &pb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, []string{"01-Mar-2026", "02-Mar-2026"}, resp.Dates)
}

func TestServer_GetExistingDates_InternalError(t *testing.T) {
	srv := newServer(&mockCalendarStore{
		getExistingDatesFn: func() ([]string, error) {
			return nil, errors.New("disk error")
		},
	}, nil, nil, nil)

	_, err := srv.GetExistingDates(context.Background(), &pb.Empty{})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- EnsureNote ---

func TestServer_EnsureNote_Success(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		ensureNoteFn: func(date string) error { return nil },
	}, nil, nil)

	resp, err := srv.EnsureNote(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestServer_EnsureNote_InternalError(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		ensureNoteFn: func(date string) error { return errors.New("write error") },
	}, nil, nil)

	_, err := srv.EnsureNote(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- GetNote ---

func TestServer_GetNote_ReturnsContent(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "note content", nil },
	}, nil, nil)

	resp, err := srv.GetNote(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.NoError(t, err)
	assert.Equal(t, "note content", resp.Content)
}

func TestServer_GetNote_NotFound(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "", nil },
	}, nil, nil)

	_, err := srv.GetNote(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestServer_GetNote_InternalError(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "", errors.New("io error") },
	}, nil, nil)

	_, err := srv.GetNote(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- GetRating ---

func TestServer_GetRating_ReturnsRating(t *testing.T) {
	rating := 7
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "content", nil },
	}, &mockRatingStore{
		getRatingFn: func(content string) *int { return &rating },
	}, nil)

	resp, err := srv.GetRating(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.NoError(t, err)
	assert.True(t, resp.HasRating)
	assert.Equal(t, int32(7), resp.Rating)
}

func TestServer_GetRating_NoRatingField(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "content", nil },
	}, &mockRatingStore{
		getRatingFn: func(content string) *int { return nil },
	}, nil)

	resp, err := srv.GetRating(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.NoError(t, err)
	assert.False(t, resp.HasRating)
}

func TestServer_GetRating_MissingNote(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "", nil },
	}, nil, nil)

	resp, err := srv.GetRating(context.Background(), &pb.DateRequest{Date: "01-Jan-1999"})
	require.NoError(t, err) // не ошибка — просто нет данных
	assert.False(t, resp.HasRating)
}

// --- UpdateRating ---

func TestServer_UpdateRating_Success(t *testing.T) {
	var receivedDate string
	var receivedRating int
	srv := newServer(nil, nil, &mockRatingStore{
		updateRatingFn: func(date string, rating int) error {
			receivedDate, receivedRating = date, rating
			return nil
		},
	}, nil)

	resp, err := srv.UpdateRating(context.Background(), &pb.UpdateRatingRequest{Date: "01-Mar-2026", Rating: 8})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "01-Mar-2026", receivedDate)
	assert.Equal(t, 8, receivedRating)
}

func TestServer_UpdateRating_Error(t *testing.T) {
	srv := newServer(nil, nil, &mockRatingStore{
		updateRatingFn: func(date string, rating int) error { return errors.New("write error") },
	}, nil)

	resp, err := srv.UpdateRating(context.Background(), &pb.UpdateRatingRequest{Date: "01-Mar-2026", Rating: 8})
	require.NoError(t, err) // gRPC-ошибки нет — Success: false
	assert.False(t, resp.Success)
}

// --- GetTasks ---

func TestServer_GetTasks_ReturnsMappedTasks(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "content", nil },
	}, nil, &mockTaskStore{
		parseTasksFn: func(content string) []features.Task {
			return []features.Task{
				{Text: "Buy milk", Completed: false, Index: 0, LineNumber: 5},
				{Text: "Walk dog", Completed: true, Index: 1, LineNumber: 6},
			}
		},
	})

	resp, err := srv.GetTasks(context.Background(), &pb.DateRequest{Date: "01-Mar-2026"})
	require.NoError(t, err)
	require.Len(t, resp.Tasks, 2)
	assert.Equal(t, "Buy milk", resp.Tasks[0].Text)
	assert.False(t, resp.Tasks[0].Completed)
	assert.Equal(t, int32(0), resp.Tasks[0].Index)
	assert.Equal(t, int32(5), resp.Tasks[0].LineNumber)
	assert.True(t, resp.Tasks[1].Completed)
}

func TestServer_GetTasks_EmptyWhenNoteNotFound(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		readNoteFn: func(date string) (string, error) { return "", nil },
	}, nil, nil)

	resp, err := srv.GetTasks(context.Background(), &pb.DateRequest{Date: "01-Jan-1999"})
	require.NoError(t, err)
	assert.Empty(t, resp.Tasks)
}

// --- ToggleTask ---

func TestServer_ToggleTask_Success(t *testing.T) {
	var receivedIndex int
	srv := newServer(nil, nil, nil, &mockTaskStore{
		toggleTaskFn: func(date string, index int) error {
			receivedIndex = index
			return nil
		},
	})

	resp, err := srv.ToggleTask(context.Background(), &pb.ToggleTaskRequest{Date: "01-Mar-2026", TaskIndex: 2})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 2, receivedIndex)
}

func TestServer_ToggleTask_Error(t *testing.T) {
	srv := newServer(nil, nil, nil, &mockTaskStore{
		toggleTaskFn: func(date string, index int) error { return errors.New("index out of range") },
	})

	resp, err := srv.ToggleTask(context.Background(), &pb.ToggleTaskRequest{Date: "01-Mar-2026", TaskIndex: 99})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}

// --- AddTask ---

func TestServer_AddTask_Success(t *testing.T) {
	var receivedText string
	srv := newServer(nil, nil, nil, &mockTaskStore{
		addTaskFn: func(date, text string) error {
			receivedText = text
			return nil
		},
	})

	resp, err := srv.AddTask(context.Background(), &pb.AddTaskRequest{Date: "01-Mar-2026", TaskText: "New task"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "New task", receivedText)
}

func TestServer_AddTask_Error(t *testing.T) {
	srv := newServer(nil, nil, nil, &mockTaskStore{
		addTaskFn: func(date, text string) error { return errors.New("write error") },
	})

	resp, err := srv.AddTask(context.Background(), &pb.AddTaskRequest{Date: "01-Mar-2026", TaskText: "Task"})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}

// --- AppendToNote ---

func TestServer_AppendToNote_Success(t *testing.T) {
	var receivedText string
	srv := newServer(nil, &mockNoteStore{
		appendToNoteFn: func(date, text string) error {
			receivedText = text
			return nil
		},
	}, nil, nil)

	resp, err := srv.AppendToNote(context.Background(), &pb.AppendRequest{Date: "01-Mar-2026", Text: "Hello"})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "Hello", receivedText)
}

func TestServer_AppendToNote_Error(t *testing.T) {
	srv := newServer(nil, &mockNoteStore{
		appendToNoteFn: func(date, text string) error { return errors.New("disk full") },
	}, nil, nil)

	resp, err := srv.AppendToNote(context.Background(), &pb.AppendRequest{Date: "01-Mar-2026", Text: "Hello"})
	require.NoError(t, err)
	assert.False(t, resp.Success)
}
