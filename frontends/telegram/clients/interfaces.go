package clients

import (
	"context"
	"io"

	pb "notes-bot/proto/notifications"
)

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
	AppendToNoteByPath(ctx context.Context, relpath, text string) (bool, error)
	ListDirectory(ctx context.Context, relpath string) ([]DirEntry, error)
	GetNoteByPath(ctx context.Context, relpath string) (string, error)
}

// SearchService is the interface for the search gRPC service.
type SearchService interface {
	SearchByName(ctx context.Context, query string, limit int) ([]*SearchHit, error)
	SearchByContent(ctx context.Context, query string, limit int) ([]*SearchHit, error)
	SearchSemantic(ctx context.Context, query string, limit int) ([]*SearchHit, error)
	GetNoteByID(ctx context.Context, id int64) (*SearchNote, error)
}

// NotificationsService is the interface for the notifications gRPC service.
type NotificationsService interface {
	CreateReminder(ctx context.Context, userID int64, title, scheduleType string, scheduleParams *pb.ScheduleParams, createTask bool) (*ReminderInfo, error)
	ListReminders(ctx context.Context, userID int64) ([]*ReminderInfo, error)
	DeleteReminder(ctx context.Context, reminderID, userID int64) (bool, error)
	PostponeReminder(ctx context.Context, reminderID, userID int64, postponeMinutes int32) (*ReminderInfo, error)
	StoreLocation(ctx context.Context, userID int64, lat, lon, accuracy, altitude, heading, speed float64, source string, liveMsgID int64) (*LocationInfo, error)
	GetLatestLocation(ctx context.Context, userID int64) (*LocationInfo, error)
	GetLocationHistory(ctx context.Context, userID int64, limit, offset int) ([]*LocationInfo, error)
	ToggleLocationTracking(ctx context.Context, userID int64, active bool) (bool, error)
	GetLocationTrackingStatus(ctx context.Context, userID int64) (bool, error)
}

// WhisperService is the interface for the whisper transcription gRPC service.
type WhisperService interface {
	Submit(ctx context.Context, r io.Reader, format, preset string) (jobID string, queuePosition int, err error)
	GetStatus(ctx context.Context, jobID string) (*JobResult, error)
	Cancel(ctx context.Context, jobID string) (bool, error)
}

// Ensure concrete types satisfy their interfaces at compile time.
var _ CoreService = (*CoreClient)(nil)
var _ NotificationsService = (*NotificationsClient)(nil)
var _ WhisperService = (*WhisperClient)(nil)
var _ SearchService = (*SearchClient)(nil)
