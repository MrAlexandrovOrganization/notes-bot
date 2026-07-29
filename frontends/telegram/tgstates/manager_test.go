package tgstates

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryStore is an in-memory StateStore for testing handlers.
// It mimics the read-modify-write semantics of StateManager without Redis.
type memoryStore struct {
	mu       sync.Mutex
	contexts map[int64]*UserContext
}

func newMemoryStore() *memoryStore {
	return &memoryStore{contexts: make(map[int64]*UserContext)}
}

func (s *memoryStore) GetContext(_ context.Context, userID int64) (*UserContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getContextLocked(userID)
}

func (s *memoryStore) getContextLocked(userID int64) (*UserContext, error) {
	uc, ok := s.contexts[userID]
	if !ok {
		now := time.Now()
		uc = &UserContext{
			UserID:        userID,
			State:         StateIdle,
			ActiveDate:    "01-Jan-2025",
			CalendarMonth: int(now.Month()),
			CalendarYear:  now.Year(),
		}
		s.contexts[userID] = uc
	}
	cp := *uc
	return &cp, nil
}

func (s *memoryStore) UpdateContext(_ context.Context, userID int64, updates func(*UserContext)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uc, err := s.getContextLocked(userID)
	if err != nil {
		return err
	}
	updates(uc)
	s.contexts[userID] = uc
	return nil
}

func (s *memoryStore) SetActiveDate(ctx context.Context, userID int64, date string) error {
	return s.UpdateContext(ctx, userID, func(uc *UserContext) {
		uc.ActiveDate = date
	})
}

func (s *memoryStore) snapshot(userID int64) *UserContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	uc, ok := s.contexts[userID]
	if !ok {
		return nil
	}
	cp := *uc
	return &cp
}

// Compile-time check that memoryStore implements StateStore.
var _ StateStore = (*memoryStore)(nil)

func TestMemoryStore_GetContext_CreatesDefault(t *testing.T) {
	store := newMemoryStore()
	uc, err := store.GetContext(t.Context(), 42)
	require.NoError(t, err)
	assert.Equal(t, StateIdle, uc.State)
	assert.Equal(t, "01-Jan-2025", uc.ActiveDate)
}

func TestMemoryStore_UpdateContext(t *testing.T) {
	store := newMemoryStore()
	err := store.UpdateContext(t.Context(), 1, func(uc *UserContext) {
		uc.State = StateWaitingRating
		uc.ActiveDate = "15-Nov-2025"
	})
	require.NoError(t, err)

	uc := store.snapshot(1)
	assert.Equal(t, StateWaitingRating, uc.State)
	assert.Equal(t, "15-Nov-2025", uc.ActiveDate)
}

func TestMemoryStore_UpdateContext_SequenceDoesNotLoseData(t *testing.T) {
	store := newMemoryStore()

	err := store.UpdateContext(t.Context(), 1, func(uc *UserContext) {
		uc.State = StateReminderCreateTitle
	})
	require.NoError(t, err)

	err = store.UpdateContext(t.Context(), 1, func(uc *UserContext) {
		uc.ReminderDraft.Title = "Test"
	})
	require.NoError(t, err)

	err = store.UpdateContext(t.Context(), 1, func(uc *UserContext) {
		uc.ReminderDraft.ScheduleType = "daily"
	})
	require.NoError(t, err)

	uc := store.snapshot(1)
	assert.Equal(t, StateReminderCreateTitle, uc.State)
	assert.Equal(t, "Test", uc.ReminderDraft.Title)
	assert.Equal(t, "daily", uc.ReminderDraft.ScheduleType)
}

func TestMemoryStore_SetActiveDate(t *testing.T) {
	store := newMemoryStore()
	err := store.SetActiveDate(t.Context(), 1, "09-Nov-2025")
	require.NoError(t, err)
	uc := store.snapshot(1)
	assert.Equal(t, "09-Nov-2025", uc.ActiveDate)
}

func TestMemoryStore_ConcurrentUpdates_NoRace(t *testing.T) {
	store := newMemoryStore()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = store.UpdateContext(t.Context(), 1, func(uc *UserContext) {
				uc.TaskPage = 1
			})
		}()
	}
	wg.Wait()
}

func TestUserContext_JSONRoundTrip(t *testing.T) {
	uc := UserContext{
		UserID:        42,
		State:         StateReminderCreateNL,
		ActiveDate:    "09-Nov-2025",
		CalendarMonth: 11,
		CalendarYear:  2025,
		ReminderDraft: ReminderDraft{
			Title:        "Позвонить маме",
			ScheduleType: "weekly",
			Hour:         9,
			Minute:       30,
			Days:         []int{0, 2, 4},
		},
		SmartDraft: SmartDraft{
			RawText:    "завтра в 9",
			Intent:     "reminder",
			Confidence: 0.95,
		},
	}

	data, err := json.Marshal(uc)
	require.NoError(t, err)

	var restored UserContext
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, uc, restored)
}
