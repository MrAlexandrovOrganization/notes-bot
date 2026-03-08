package tgstates

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const ttl = 7 * 24 * time.Hour

// StateManager manages user contexts backed by Redis.
type StateManager struct {
	redis               *redis.Client
	timezoneOffsetHours int
	dayStartHour        int
}

func NewStateManager(rdb *redis.Client, tzOffset, dayStartHour int) *StateManager {
	return &StateManager{redis: rdb, timezoneOffsetHours: tzOffset, dayStartHour: dayStartHour}
}

func (m *StateManager) key(userID int64) string {
	return fmt.Sprintf("user_state:%d", userID)
}

func (m *StateManager) todayDate() string {
	tz := time.FixedZone("local", m.timezoneOffsetHours*3600)
	local := time.Now().In(tz)
	if local.Hour() < m.dayStartHour {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("02-Jan-2006")
}

// GetContext retrieves or creates a UserContext for the given user.
func (m *StateManager) GetContext(ctx context.Context, userID int64) (*UserContext, error) {
	data, err := m.redis.Get(ctx, m.key(userID)).Bytes()
	if err == nil {
		var uc UserContext
		if err := json.Unmarshal(data, &uc); err == nil {
			return &uc, nil
		}
	}

	now := time.Now()
	uc := &UserContext{
		UserID:        userID,
		State:         StateIdle,
		ActiveDate:    m.todayDate(),
		CalendarMonth: int(now.Month()),
		CalendarYear:  now.Year(),
		ReminderDraft: map[string]any{},
	}
	if err := m.save(ctx, uc); err != nil {
		return nil, err
	}
	return uc, nil
}

// UpdateContext applies field updates to the user context and persists it.
func (m *StateManager) UpdateContext(ctx context.Context, userID int64, updates func(*UserContext)) error {
	uc, err := m.GetContext(ctx, userID)
	if err != nil {
		return err
	}
	updates(uc)
	return m.save(ctx, uc)
}

// ResetContext resets the user to IDLE state, clearing task page and last message.
func (m *StateManager) ResetContext(ctx context.Context, userID int64) error {
	return m.UpdateContext(ctx, userID, func(uc *UserContext) {
		uc.State = StateIdle
		uc.TaskPage = 0
		uc.LastMessageID = 0
	})
}

// SetActiveDate sets the active date for a user.
func (m *StateManager) SetActiveDate(ctx context.Context, userID int64, date string) error {
	return m.UpdateContext(ctx, userID, func(uc *UserContext) {
		uc.ActiveDate = date
	})
}

func (m *StateManager) save(ctx context.Context, uc *UserContext) error {
	data, err := json.Marshal(uc)
	if err != nil {
		return fmt.Errorf("marshal context: %w", err)
	}
	return m.redis.SetEx(ctx, m.key(uc.UserID), data, ttl).Err()
}
