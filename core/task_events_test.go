package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/core/features"
	"notes-bot/internal/automationevents"
	pb "notes-bot/proto/automation/v1"
)

func TestTaskEventsIdentityAndAcknowledgement(t *testing.T) {
	daily := setupNotesEnv(t)
	ctx := t.Context()
	date := "12-Nov-2026"
	notes := &realNoteStore{}
	tasks := &realTaskStore{emitEvents: true}
	require.NoError(t, notes.EnsureNote(ctx, date))
	require.NoError(t, tasks.AddTask(ctx, date, "Synthetic <task>"))
	tracked, err := tasks.TrackTask(ctx, date, 0, "Synthetic <task>")
	require.NoError(t, err)
	require.NotEmpty(t, tracked.ID)
	again, err := tasks.TrackTask(ctx, date, 0, "Synthetic <task>")
	require.NoError(t, err)
	require.Equal(t, tracked.ID, again.ID)
	require.NoError(t, tasks.SetTaskStatus(ctx, date, 0, features.TaskStateCompleted))
	path := filepath.Join(daily, date+".md")
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, tasks.SetTaskStatus(ctx, date, 0, features.TaskStateCompleted))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NoError(t, tasks.AddTask(ctx, date, "Other task"))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(string(content), "\n")
	parsed := features.ParseTasks(ctx, string(content))
	i, j := parsed[0].LineNumber-1, parsed[1].LineNumber-1
	lines[i], lines[j] = lines[j], lines[i]
	require.NoError(t, writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0644))
	require.NoError(t, tasks.SetTaskStatus(ctx, date, 1, features.TaskStateRejected))
	server := &TaskEventsServer{Enabled: true}
	batch, err := server.ReadEvents(ctx, &pb.ReadEventsRequest{})
	require.NoError(t, err)
	require.Len(t, batch.Events, 2)
	for i, e := range batch.Events {
		require.Equal(t, tracked.ID, e.GetTaskStatusChanged().TaskId)
		require.EqualValues(t, i+1, e.GetTaskStatusChanged().Revision)
		require.Equal(t, "Synthetic <task>", e.GetTaskStatusChanged().Title)
	}
	changed := proto.Clone(batch.Events[0]).(*pb.Event)
	changed.GetTaskStatusChanged().Title = "changed"
	_, err = server.AckEvent(ctx, &pb.AckEventRequest{Event: changed})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	for _, e := range batch.Events {
		_, err = server.AckEvent(ctx, &pb.AckEventRequest{Event: e})
		require.NoError(t, err)
		_, err = server.AckEvent(ctx, &pb.AckEventRequest{Event: e})
		require.NoError(t, err)
	}
	empty, err := server.ReadEvents(ctx, &pb.ReadEventsRequest{})
	require.NoError(t, err)
	require.Empty(t, empty.Events)
}
func TestTaskEventRecoveryBeforeAndAfterFileCommit(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-write", true: "after-write"}[applied], func(t *testing.T) {
			daily := setupNotesEnv(t)
			ctx := t.Context()
			date := "12-Nov-2026"
			notes := &realNoteStore{}
			tasks := &realTaskStore{emitEvents: true}
			require.NoError(t, notes.EnsureNote(ctx, date))
			require.NoError(t, tasks.AddTask(ctx, date, "Recoverable task"))
			task, err := tasks.TrackTask(ctx, date, 0, "Recoverable task")
			require.NoError(t, err)
			path := filepath.Join(daily, date+".md")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			event := &pb.Event{Id: uuid.NewString(), Source: automationevents.CoreSource, Type: automationevents.TaskStatusChangedType, Subject: automationevents.Subject(date, task.ID), OccurredAt: timestamppb.Now(), Data: &pb.Event_TaskStatusChanged{TaskStatusChanged: &pb.TaskStatusChanged{Date: date, TaskId: task.ID, Title: task.Text, Before: pb.TaskStatus_TASK_STATUS_PENDING, After: pb.TaskStatus_TASK_STATUS_COMPLETED, Revision: 1}}}
			after, err := features.SetTaskStatusContent(ctx, string(before), 0, features.TaskStateCompleted)
			require.NoError(t, err)
			after = markTask(after, task, task.ID, 1, event.Id)
			eventJSON, err := protojson.Marshal(event)
			require.NoError(t, err)
			raw, err := json.Marshal(taskIntent{BeforeHash: taskContentHash(before), AfterHash: taskContentHash([]byte(after)), Event: eventJSON})
			require.NoError(t, err)
			require.NoError(t, writeFileAtomic(taskIntentPath(path), raw, 0600))
			if applied {
				require.NoError(t, writeFileAtomic(path, []byte(after), 0644))
				require.NoError(t, notes.AppendToNote(ctx, date, "Concurrent ordinary append"))
			}
			server := &TaskEventsServer{Enabled: true}
			batch, err := server.ReadEvents(ctx, &pb.ReadEventsRequest{})
			require.NoError(t, err)
			if applied {
				require.Len(t, batch.Events, 1)
				require.True(t, proto.Equal(event, batch.Events[0]))
			} else {
				require.Empty(t, batch.Events)
			}
			_, err = os.Stat(taskIntentPath(path))
			require.True(t, os.IsNotExist(err))
		})
	}
}
func TestConcurrentTaskChangesPublishEachTransition(t *testing.T) {
	setupNotesEnv(t)
	ctx := t.Context()
	date := "12-Nov-2026"
	notes := &realNoteStore{}
	tasks := &realTaskStore{emitEvents: true}
	require.NoError(t, notes.EnsureNote(ctx, date))
	require.NoError(t, tasks.AddTask(ctx, date, "Concurrent task"))
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Go(func() { errs <- tasks.ToggleTask(ctx, date, 0) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	batch, err := (&TaskEventsServer{Enabled: true}).ReadEvents(ctx, &pb.ReadEventsRequest{})
	require.NoError(t, err)
	require.Len(t, batch.Events, 12)
	seen := map[string]bool{}
	for i, e := range batch.Events {
		require.EqualValues(t, i+1, e.GetTaskStatusChanged().Revision)
		require.False(t, seen[e.Id])
		seen[e.Id] = true
	}
}
func TestTrackTaskRejectsStaleSelection(t *testing.T) {
	daily := setupNotesEnv(t)
	ctx := t.Context()
	notes := &realNoteStore{}
	tasks := &realTaskStore{emitEvents: true}
	date := "12-Nov-2026"
	require.NoError(t, notes.EnsureNote(ctx, date))
	require.NoError(t, tasks.AddTask(ctx, date, "Task"))
	path := filepath.Join(daily, date+".md")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = tasks.TrackTask(ctx, date, 0, "Different task")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
