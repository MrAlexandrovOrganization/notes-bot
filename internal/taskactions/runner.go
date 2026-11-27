package taskactions

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"notes-bot/internal/timeutil"
	pb "notes-bot/proto/notes"
)

const (
	Notify     = "notify"
	CreateTask = "create_task"
)

type Day struct {
	TimezoneOffsetHours int
	StartHour           int
}

func (d Day) Date(at time.Time) string {
	return timeutil.TodayDateAt(at, d.TimezoneOffsetHours, d.StartHour)
}

func ValidTitle(text string) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= 500 && strings.TrimSpace(text) != "" &&
		!strings.ContainsAny(text, "\r\n\x00") && !strings.Contains(text, "---") &&
		!strings.Contains(text, "<!-- notes-bot:") && !strings.Contains(text, "[completion::")
}

type Job struct {
	ID                                   int64
	LeaseToken, Date, Text, OperationKey string
	ExpiresAt                            time.Time
}

type Queue interface {
	ClaimTask(context.Context, int64) (*Job, error)
	CompleteTask(context.Context, int64, int64, string, bool) error
}

// Run executes due task actions independently of notification delivery and event ingestion.
func Run(ctx context.Context, core pb.NotesServiceClient, queue Queue, source string, owner int64, onError func()) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		for i := 0; i < 10; i++ {
			found, err := runOne(ctx, core, queue, source, owner)
			if err != nil && ctx.Err() == nil {
				onError()
			}
			if !found || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runOne(ctx context.Context, core pb.NotesServiceClient, queue Queue, source string, owner int64) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	job, err := queue.ClaimTask(claimCtx, owner)
	cancel()
	if err != nil || job == nil {
		return false, err
	}
	deadline := time.Now().Add(15 * time.Second)
	if job.ExpiresAt.Before(deadline) {
		deadline = job.ExpiresAt
	}
	writeCtx, cancel := context.WithDeadline(ctx, deadline)
	_, err = core.AddTaskOnce(writeCtx, &pb.AddTaskOnceRequest{
		Date: job.Date, TaskText: job.Text, OperationId: "task-action:" + source + ":" + job.OperationKey,
	})
	cancel()
	ackCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return true, errors.Join(err, queue.CompleteTask(ackCtx, owner, job.ID, job.LeaseToken, err == nil))
}
