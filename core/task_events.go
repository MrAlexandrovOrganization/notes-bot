package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/core/features"
	"notes-bot/internal/automationevents"
	"notes-bot/internal/taskidentity"
	apb "notes-bot/proto/automation/v1"
	pb "notes-bot/proto/notes"
)

var errTaskIndex = errors.New("invalid task index")

type taskIntent struct {
	BeforeHash string          `json:"before_hash"`
	AfterHash  string          `json:"after_hash"`
	Event      json.RawMessage `json:"event"`
}

func taskIntentPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".task-intent.json")
}
func taskContentHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func taskOutboxDir(ctx context.Context) (string, error) {
	dir, err := resolveVaultPath(GetConfig(ctx).NotesDir, ".notes-bot-task-events")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err = syncDirectory(filepath.Dir(dir)); err != nil {
		return "", err
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Lstat(ignore); os.IsNotExist(err) {
		if err = writeFileAtomic(ignore, []byte("*\n"), 0600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return dir, nil
}
func writeTaskEvent(ctx context.Context, e *apb.Event) error {
	name, err := automationevents.Filename(e)
	if err != nil {
		return err
	}
	dir, err := taskOutboxDir(ctx)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	raw, err := protojson.Marshal(e)
	if err != nil {
		return err
	}
	if old, err := readTaskRecord(path); err == nil {
		existing := new(apb.Event)
		if protojson.Unmarshal(old, existing) != nil || !proto.Equal(existing, e) {
			return errors.New("task event conflict")
		}
		return syncDirectory(dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(path, raw, 0600)
}

// recoverTaskIntent runs under the note lock. A durable event is published only
// when the note hash or task's immutable event stamp proves the write occurred.
func recoverTaskIntent(ctx context.Context, path string) error {
	intentPath := taskIntentPath(path)
	raw, err := readTaskRecord(intentPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var intent taskIntent
	if err = json.Unmarshal(raw, &intent); err != nil {
		return err
	}
	e := new(apb.Event)
	if err = protojson.Unmarshal(intent.Event, e); err != nil {
		return err
	}
	if err = automationevents.Validate(e); err != nil {
		return err
	}
	if filepath.Base(path) != e.GetTaskStatusChanged().Date+".md" {
		return errors.New("task intent destination conflict")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := taskContentHash(content)
	applied := hash == intent.AfterHash
	if !applied && hash != intent.BeforeHash {
		change := e.GetTaskStatusChanged()
		found := false
		for _, task := range features.ParseTasks(ctx, string(content)) {
			if task.ID == change.TaskId {
				if found {
					return errors.New("duplicate task id")
				}
				found = true
				if task.Revision == change.Revision && task.LastEventID == e.Id {
					applied = true
				} else if task.Revision != change.Revision-1 || featureTaskStatus(task.State) != change.Before {
					return errors.New("task recovery conflict")
				}
			}
		}
		if !found {
			return errors.New("task recovery cannot locate target")
		}
	}
	if applied {
		if err = syncDirectory(filepath.Dir(path)); err != nil {
			return err
		}
		if err = writeTaskEvent(ctx, e); err != nil {
			return err
		}
	}
	if err = os.Remove(intentPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func featureTaskStatus(state features.TaskState) apb.TaskStatus {
	switch state {
	case features.TaskStateCompleted:
		return apb.TaskStatus_TASK_STATUS_COMPLETED
	case features.TaskStateRejected:
		return apb.TaskStatus_TASK_STATUS_REJECTED
	default:
		return apb.TaskStatus_TASK_STATUS_PENDING
	}
}
func validateTaskIDs(tasks []features.Task) error {
	seen := map[string]bool{}
	for _, t := range tasks {
		if t.ID == "" {
			continue
		}
		if _, err := uuid.Parse(t.ID); err != nil || seen[t.ID] {
			return errors.New("invalid or duplicate task id")
		}
		seen[t.ID] = true
	}
	return nil
}
func markTask(content string, t features.Task, id string, revision uint64, event string) string {
	lines := strings.Split(content, "\n")
	lines[t.LineNumber-1] = taskidentity.WithTaskIdentity(lines[t.LineNumber-1], id, revision, event)
	return strings.Join(lines, "\n")
}
func (r *realTaskStore) changeWithEvent(ctx context.Context, date string, index int, target *features.TaskState) error {
	path, err := dailyNotePath(ctx, date)
	if err != nil {
		return err
	}
	unlock, err := lockFile(ctx, path)
	if err != nil {
		return err
	}
	defer unlock()
	if err = recoverTaskIntent(ctx, path); err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	tasks := features.ParseTasks(ctx, string(content))
	if err = validateTaskIDs(tasks); err != nil {
		return err
	}
	if index < 0 || index >= len(tasks) {
		return errTaskIndex
	}
	task := tasks[index]
	next := features.TaskStatePending
	if target != nil {
		next = *target
	} else {
		switch task.State {
		case features.TaskStatePending:
			next = features.TaskStateCompleted
		case features.TaskStateCompleted:
			next = features.TaskStateRejected
		}
	}
	if next == task.State {
		return nil
	}
	if task.Revision >= math.MaxInt64 {
		return errors.New("task revision exhausted")
	}
	id := task.ID
	if id == "" {
		id = uuid.NewString()
	}
	updated, err := features.SetTaskStatusContent(ctx, string(content), index, next)
	if err != nil {
		return err
	}
	eventID := uuid.NewString()
	revision := task.Revision + 1
	updated = markTask(updated, task, id, revision, eventID)
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	e := &apb.Event{Id: eventID, Source: automationevents.CoreSource, Type: automationevents.TaskStatusChangedType, Subject: automationevents.Subject(date, id), OccurredAt: timestamppb.Now(), Traceparent: carrier["traceparent"], Tracestate: carrier["tracestate"], Data: &apb.Event_TaskStatusChanged{TaskStatusChanged: &apb.TaskStatusChanged{Date: date, TaskId: id, Title: automationevents.TaskLabel(task.Text), Before: featureTaskStatus(task.State), After: featureTaskStatus(next), Revision: revision}}}
	eventJSON, err := protojson.Marshal(e)
	if err != nil {
		return err
	}
	intent := taskIntent{BeforeHash: taskContentHash(content), AfterHash: taskContentHash([]byte(updated)), Event: eventJSON}
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	if err = writeFileAtomic(taskIntentPath(path), raw, 0600); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err = writeFileAtomic(path, []byte(updated), info.Mode().Perm()); err != nil {
		return err
	}
	return recoverTaskIntent(ctx, path)
}
func (r *realTaskStore) ToggleTaskAtomically(ctx context.Context, date string, index int) error {
	return r.ToggleTask(ctx, date, index)
}
func (r *realTaskStore) TrackTask(ctx context.Context, date string, index int, expected string) (features.Task, error) {
	if !r.emitEvents {
		return features.Task{}, status.Error(codes.FailedPrecondition, "task events are disabled")
	}
	path, err := dailyNotePath(ctx, date)
	if err != nil {
		return features.Task{}, err
	}
	unlock, err := lockFile(ctx, path)
	if err != nil {
		return features.Task{}, err
	}
	defer unlock()
	if err = recoverTaskIntent(ctx, path); err != nil {
		return features.Task{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return features.Task{}, err
	}
	tasks := features.ParseTasks(ctx, string(content))
	if err = validateTaskIDs(tasks); err != nil {
		return features.Task{}, err
	}
	if index < 0 || index >= len(tasks) || tasks[index].Text != expected {
		return features.Task{}, status.Error(codes.FailedPrecondition, "task changed; select it again")
	}
	task := tasks[index]
	if task.ID != "" {
		return task, nil
	}
	task.ID = uuid.NewString()
	updated := markTask(string(content), task, task.ID, 0, "")
	info, err := os.Stat(path)
	if err != nil {
		return features.Task{}, err
	}
	return task, writeFileAtomic(path, []byte(updated), info.Mode().Perm())
}
func (s *NotesServer) TrackTask(ctx context.Context, r *pb.TrackTaskRequest) (*pb.TaskResponse, error) {
	if err := validateDate(r.GetDate()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid date")
	}
	store, ok := s.tasks.(interface {
		TrackTask(context.Context, string, int, string) (features.Task, error)
	})
	if !ok {
		return nil, status.Error(codes.Unimplemented, "task tracking unavailable")
	}
	task, err := store.TrackTask(ctx, r.Date, int(r.TaskIndex), r.ExpectedText)
	if err != nil {
		if _, ok := status.FromError(err); ok {
			return nil, err
		}
		return nil, status.Error(codes.Unavailable, "task tracking failed")
	}
	state := pb.TaskState_TASK_STATE_PENDING
	if task.Completed() {
		state = pb.TaskState_TASK_STATE_COMPLETED
	} else if task.Rejected() {
		state = pb.TaskState_TASK_STATE_INCOMPLETE
	}
	return &pb.TaskResponse{Task: &pb.Task{Id: task.ID, Text: task.Text, State: state, Index: int32(task.Index), LineNumber: int32(task.LineNumber)}}, nil
}

type TaskEventsServer struct {
	apb.UnimplementedCoreEventsServiceServer
	Enabled bool
}

func (s *TaskEventsServer) ReadEvents(ctx context.Context, r *apb.ReadEventsRequest) (*apb.ReadEventsResponse, error) {
	if !s.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "task events are disabled")
	}
	limit := r.GetLimit()
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, status.Error(codes.InvalidArgument, "invalid batch size")
	}
	// Recover before serving events, including an interrupted first publication.
	intents, err := filepath.Glob(filepath.Join(GetConfig(ctx).DailyNotesDir, ".*.md.task-intent.json"))
	if err != nil {
		return nil, status.Error(codes.Unavailable, "outbox unavailable")
	}
	for _, intent := range intents {
		path := filepath.Join(filepath.Dir(intent), strings.TrimSuffix(strings.TrimPrefix(filepath.Base(intent), "."), ".task-intent.json"))
		unlock, err := lockFile(ctx, path)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "outbox recovery unavailable")
		}
		err = recoverTaskIntent(ctx, path)
		unlock()
		if err != nil {
			return nil, status.Error(codes.Unavailable, "outbox recovery failed")
		}
	}
	dir, err := taskOutboxDir(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "outbox unavailable")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "outbox unavailable")
	}
	out := &apb.ReadEventsResponse{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, status.Error(codes.Unavailable, "invalid outbox entry")
		}
		raw, err := readTaskRecord(filepath.Join(dir, entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, status.Error(codes.Unavailable, "outbox read failed")
		}
		e := new(apb.Event)
		if protojson.Unmarshal(raw, e) != nil {
			return nil, status.Error(codes.Unavailable, "invalid outbox event")
		}
		name, err := automationevents.Filename(e)
		if err != nil || name != entry.Name() {
			return nil, status.Error(codes.Unavailable, "invalid outbox identity")
		}
		out.Events = append(out.Events, e)
		if len(out.Events) >= int(limit) {
			break
		}
	}
	return out, nil
}
func (s *TaskEventsServer) AckEvent(ctx context.Context, r *apb.AckEventRequest) (*apb.AckEventResponse, error) {
	if !s.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "task events disabled")
	}
	name, err := automationevents.Filename(r.GetEvent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid event")
	}
	dir, err := taskOutboxDir(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "outbox unavailable")
	}
	path := filepath.Join(dir, name)
	raw, err := readTaskRecord(path)
	if os.IsNotExist(err) {
		if err := syncDirectory(dir); err != nil {
			return nil, status.Error(codes.Unavailable, "outbox acknowledgement sync failed")
		}
		return &apb.AckEventResponse{}, nil
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, "outbox read failed")
	}
	e := new(apb.Event)
	if protojson.Unmarshal(raw, e) != nil || !proto.Equal(e, r.Event) {
		return nil, status.Error(codes.FailedPrecondition, "event acknowledgement conflict")
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, status.Error(codes.Unavailable, "outbox acknowledgement failed")
	}
	if syncDirectory(dir) != nil {
		return nil, status.Error(codes.Unavailable, "outbox acknowledgement sync failed")
	}
	return &apb.AckEventResponse{}, nil
}

func readTaskRecord(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return nil, err
	}
	if len(raw) > 65536 {
		return nil, errors.New("task metadata record too large")
	}
	return raw, nil
}

// PrepareTaskEvents validates persistent outbox access before accepting mutations.
func PrepareTaskEvents(ctx context.Context) error {
	dir, err := taskOutboxDir(ctx)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".startup-check-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return syncDirectory(dir)
}
