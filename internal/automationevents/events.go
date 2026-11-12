package automationevents

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	pb "notes-bot/proto/automation/v1"
)

const TaskStatusChangedType = "notes.task.status_changed.v1"
const CoreSource = "notes-core"

func Subject(date, id string) string { return "task/" + date + "/" + id }
func ValidStatus(s pb.TaskStatus) bool {
	return s >= pb.TaskStatus_TASK_STATUS_PENDING && s <= pb.TaskStatus_TASK_STATUS_REJECTED
}
func Validate(e *pb.Event) error {
	if e == nil || e.Source != CoreSource || e.Type != TaskStatusChangedType || e.OccurredAt == nil || e.OccurredAt.CheckValid() != nil {
		return errors.New("invalid event")
	}
	if _, err := uuid.Parse(e.Id); err != nil {
		return errors.New("invalid event id")
	}
	t := e.GetTaskStatusChanged()
	if t == nil || t.Revision == 0 || t.Revision > math.MaxInt64 || !ValidStatus(t.Before) || !ValidStatus(t.After) || t.Before == t.After {
		return errors.New("invalid task transition")
	}
	if _, err := time.Parse("02-Jan-2006", t.Date); err != nil {
		return errors.New("invalid task date")
	}
	if _, err := uuid.Parse(t.TaskId); err != nil {
		return errors.New("invalid task id")
	}
	if e.Subject != Subject(t.Date, t.TaskId) {
		return errors.New("invalid task subject")
	}
	if len(t.Title) > 16000 {
		return errors.New("event title too large")
	}
	return nil
}
func Filename(e *pb.Event) (string, error) {
	if err := Validate(e); err != nil {
		return "", err
	}
	t := e.GetTaskStatusChanged()
	return fmt.Sprintf("%s_%s_%020d_%s.json", t.Date, t.TaskId, t.Revision, e.Id), nil
}

func Enabled() (bool, error) {
	switch os.Getenv("AUTOMATIONS_ENABLED") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("AUTOMATIONS_ENABLED must be true or false")
	}
}

func TaskLabel(text string) string {
	r := []rune(strings.ToValidUTF8(text, "�"))
	if len(r) > 500 {
		r = r[:500]
	}
	return string(r)
}
