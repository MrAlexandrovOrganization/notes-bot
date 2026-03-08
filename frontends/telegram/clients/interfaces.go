package clients

import "context"

// CoreService is the interface for the core notes gRPC service.
type CoreService interface {
	GetTodayDate(ctx context.Context) (string, error)
	GetExistingDates(ctx context.Context) ([]string, error)
	EnsureNote(ctx context.Context, date string) (bool, error)
	GetNote(ctx context.Context, date string) (string, error)
	GetRating(ctx context.Context, date string) (int, bool, error)
	UpdateRating(ctx context.Context, date string, rating int) (bool, error)
	GetTasks(ctx context.Context, date string) ([]*Task, error)
	ToggleTask(ctx context.Context, date string, taskIndex int) (bool, error)
	AddTask(ctx context.Context, date, taskText string) (bool, error)
	AppendToNote(ctx context.Context, date, text string) (bool, error)
}

// NotificationsService is the interface for the notifications gRPC service.
type NotificationsService interface {
	CreateReminder(ctx context.Context, userID int64, title, scheduleType, scheduleParamsJSON string, createTask bool) (*ReminderInfo, error)
	ListReminders(ctx context.Context, userID int64) ([]*ReminderInfo, error)
	DeleteReminder(ctx context.Context, reminderID, userID int64) (bool, error)
	PostponeReminder(ctx context.Context, reminderID, userID int64, postponeDays int32, targetDate string, postponeHours int32) (*ReminderInfo, error)
}

// WhisperService is the interface for the whisper transcription gRPC service.
type WhisperService interface {
	Transcribe(ctx context.Context, audioData []byte, format string) (string, error)
}

// Ensure concrete types satisfy their interfaces at compile time.
var _ CoreService = (*CoreClient)(nil)
var _ NotificationsService = (*NotificationsClient)(nil)
var _ WhisperService = (*WhisperClient)(nil)
