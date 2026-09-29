package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const emptyTasksNote = "---\ndate: \"[[01-Mar-2026]]\"\nОценка: 5\n---\n\n---\nSome text\n"

const tasksNote = "---\n" +
	"date: \"[[01-Mar-2026]]\"\n" +
	"Оценка: 5\n" +
	"---\n" +
	"- [?] Task 1\n" +
	"- [x] Task 2  [completion:: 2026-03-01]\n" +
	"- [-] Task 3\n" +
	"---\n" +
	"Some text\n"

const invalidNote = "---\nno tasks section\n---\n"

// --- ParseTasks ---

func TestParseTasks_EmptySection(t *testing.T) {
	assert.Empty(t, ParseTasks(t.Context(), emptyTasksNote))
}

func TestParseTasks_Count(t *testing.T) {
	assert.Len(t, ParseTasks(t.Context(), tasksNote), 3)
}

func TestParseTasks_PendingTask(t *testing.T) {
	tasks := ParseTasks(t.Context(), tasksNote)
	assert.Equal(t, "Task 1", tasks[0].Text)
	assert.True(t, tasks[0].Pending())
	assert.False(t, tasks[0].Completed())
	assert.Equal(t, 0, tasks[0].Index)
}

func TestParseTasks_CompletedTask(t *testing.T) {
	tasks := ParseTasks(t.Context(), tasksNote)
	assert.Equal(t, "Task 2", tasks[1].Text)
	assert.True(t, tasks[1].Completed())
	assert.Equal(t, 1, tasks[1].Index)
}

func TestParseTasks_RejectedTask(t *testing.T) {
	tasks := ParseTasks(t.Context(), tasksNote)
	assert.Equal(t, "Task 3", tasks[2].Text)
	assert.True(t, tasks[2].Rejected())
	assert.Equal(t, 2, tasks[2].Index)
}

func TestParseTasks_StripsCompletionMetadata(t *testing.T) {
	tasks := ParseTasks(t.Context(), tasksNote)
	assert.NotContains(t, tasks[1].Text, "[completion::")
}

func TestParseTasks_InvalidFormat(t *testing.T) {
	assert.Empty(t, ParseTasks(t.Context(), invalidNote))
}

func TestParseTasks_LineNumbersArePositive(t *testing.T) {
	for _, task := range ParseTasks(t.Context(), tasksNote) {
		assert.Greater(t, task.LineNumber, 0)
	}
}

// --- SetTaskStatusContent ---

func TestSetTaskStatusContent_PendingToCompleted(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 0, TaskStateCompleted)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[0].Completed())
}

func TestSetTaskStatusContent_PendingToRejected(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 0, TaskStateRejected)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[0].Rejected())
}

func TestSetTaskStatusContent_CompletedToPending(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 1, TaskStatePending)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[1].Pending())
}

func TestSetTaskStatusContent_CompletedToRejected(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 1, TaskStateRejected)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[1].Rejected())
}

func TestSetTaskStatusContent_RejectedToPending(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 2, TaskStatePending)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[2].Pending())
}

func TestSetTaskStatusContent_RejectedToCompleted(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 2, TaskStateCompleted)
	require.NoError(t, err)
	assert.True(t, ParseTasks(t.Context(), result)[2].Completed())
}

func TestSetTaskStatusContent_AddsCompletionDate(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 0, TaskStateCompleted)
	require.NoError(t, err)
	assert.Contains(t, result, "[completion::")
}

func TestSetTaskStatusContent_RemovesCompletionMetadataOnReject(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 1, TaskStateRejected)
	require.NoError(t, err)
	assert.NotContains(t, ParseTasks(t.Context(), result)[1].Text, "[completion::")
}

func TestSetTaskStatusContent_PreservesOtherTasks(t *testing.T) {
	result, err := SetTaskStatusContent(t.Context(), tasksNote, 0, TaskStateCompleted)
	require.NoError(t, err)
	tasks := ParseTasks(t.Context(), result)
	require.Len(t, tasks, 3)
	assert.Equal(t, "Task 3", tasks[2].Text)
	assert.True(t, tasks[2].Rejected())
}

func TestSetTaskStatusContent_InvalidIndex(t *testing.T) {
	_, err := SetTaskStatusContent(t.Context(), tasksNote, 99, TaskStateCompleted)
	assert.Error(t, err)
}

func TestSetTaskStatusContent_AlreadyCompleted(t *testing.T) {
	_, err := SetTaskStatusContent(t.Context(), tasksNote, 1, TaskStateCompleted)
	assert.Error(t, err)
}

func TestSetTaskStatusContent_AlreadyRejected(t *testing.T) {
	_, err := SetTaskStatusContent(t.Context(), tasksNote, 2, TaskStateRejected)
	assert.Error(t, err)
}

// --- AddTaskContent ---

func TestAddTaskContent_IncreasesCount(t *testing.T) {
	result, err := AddTaskContent(t.Context(), tasksNote, "New task")
	require.NoError(t, err)
	assert.Len(t, ParseTasks(t.Context(), result), 4)
}

func TestAddTaskContent_Text(t *testing.T) {
	result, err := AddTaskContent(t.Context(), tasksNote, "My new task")
	require.NoError(t, err)
	assert.Contains(t, taskTexts(ParseTasks(t.Context(), result)), "My new task")
}

func TestAddTaskContent_NewTaskIsPending(t *testing.T) {
	result, err := AddTaskContent(t.Context(), tasksNote, "New task")
	require.NoError(t, err)
	for _, task := range ParseTasks(t.Context(), result) {
		if task.Text == "New task" {
			assert.True(t, task.Pending())
			return
		}
	}
	t.Fatal("new task not found")
}

func TestAddTaskContent_ToEmptySection(t *testing.T) {
	result, err := AddTaskContent(t.Context(), emptyTasksNote, "First task")
	require.NoError(t, err)
	tasks := ParseTasks(t.Context(), result)
	require.Len(t, tasks, 1)
	assert.Equal(t, "First task", tasks[0].Text)
	assert.True(t, tasks[0].Pending())
}

func TestAddTaskContent_InvalidFormat(t *testing.T) {
	_, err := AddTaskContent(t.Context(), invalidNote, "Task")
	assert.Error(t, err)
}

func TestAddTaskContent_PreservesExistingTasks(t *testing.T) {
	result, err := AddTaskContent(t.Context(), tasksNote, "Extra")
	require.NoError(t, err)
	texts := taskTexts(ParseTasks(t.Context(), result))
	assert.Contains(t, texts, "Task 1")
	assert.Contains(t, texts, "Task 2")
	assert.Contains(t, texts, "Task 3")
}

// --- helpers ---

func taskTexts(tasks []Task) []string {
	texts := make([]string, len(tasks))
	for i, t := range tasks {
		texts[i] = t.Text
	}
	return texts
}
