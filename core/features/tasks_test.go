package features

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const emptyTasksNote = "---\ndate: \"[[01-Mar-2026]]\"\nОценка: 5\n---\n\n---\nSome text\n"

const tasksNote = "---\n" +
	"date: \"[[01-Mar-2026]]\"\n" +
	"Оценка: 5\n" +
	"---\n" +
	"- [ ] Task 1\n" +
	"- [x] Task 2  [completion:: 2026-03-01]\n" +
	"- [ ] Task 3\n" +
	"---\n" +
	"Some text\n"

const invalidNote = "---\nno tasks section\n---\n"

// --- ParseTasks ---

func TestParseTasks_EmptySection(t *testing.T) {
	assert.Empty(t, ParseTasks(context.Background(), emptyTasksNote))
}

func TestParseTasks_Count(t *testing.T) {
	assert.Len(t, ParseTasks(context.Background(), tasksNote), 3)
}

func TestParseTasks_IncompleteTask(t *testing.T) {
	tasks := ParseTasks(context.Background(), tasksNote)
	assert.Equal(t, "Task 1", tasks[0].Text)
	assert.False(t, tasks[0].Completed)
	assert.Equal(t, 0, tasks[0].Index)
}

func TestParseTasks_CompletedTask(t *testing.T) {
	tasks := ParseTasks(context.Background(), tasksNote)
	assert.Equal(t, "Task 2", tasks[1].Text)
	assert.True(t, tasks[1].Completed)
	assert.Equal(t, 1, tasks[1].Index)
}

func TestParseTasks_StripsCompletionMetadata(t *testing.T) {
	tasks := ParseTasks(context.Background(), tasksNote)
	assert.NotContains(t, tasks[1].Text, "[completion::")
}

func TestParseTasks_ThirdTask(t *testing.T) {
	tasks := ParseTasks(context.Background(), tasksNote)
	assert.Equal(t, "Task 3", tasks[2].Text)
	assert.False(t, tasks[2].Completed)
}

func TestParseTasks_InvalidFormat(t *testing.T) {
	assert.Empty(t, ParseTasks(context.Background(), invalidNote))
}

func TestParseTasks_LineNumbersArePositive(t *testing.T) {
	for _, task := range ParseTasks(context.Background(), tasksNote) {
		assert.Greater(t, task.LineNumber, 0)
	}
}

// --- ToggleTaskContent ---

func TestToggleTaskContent_IncompleteToComplete(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 0)
	require.NoError(t, err)
	assert.True(t, ParseTasks(context.Background(), result)[0].Completed)
}

func TestToggleTaskContent_CompleteToIncomplete(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 1)
	require.NoError(t, err)
	assert.False(t, ParseTasks(context.Background(), result)[1].Completed)
}

func TestToggleTaskContent_AddsCompletionDate(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 0)
	require.NoError(t, err)
	assert.Contains(t, result, "[completion::")
}

func TestToggleTaskContent_RemovesCompletionMetadata(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 1)
	require.NoError(t, err)
	assert.NotContains(t, ParseTasks(context.Background(), result)[1].Text, "[completion::")
}

func TestToggleTaskContent_PreservesOtherTasks(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 0)
	require.NoError(t, err)
	tasks := ParseTasks(context.Background(), result)
	require.Len(t, tasks, 3)
	assert.Equal(t, "Task 3", tasks[2].Text)
	assert.False(t, tasks[2].Completed)
}

func TestToggleTaskContent_InvalidIndex(t *testing.T) {
	_, err := ToggleTaskContent(context.Background(), tasksNote, 99)
	assert.Error(t, err)
}

func TestToggleTaskContent_Roundtrip(t *testing.T) {
	result, err := ToggleTaskContent(context.Background(), tasksNote, 0)
	require.NoError(t, err)
	result, err = ToggleTaskContent(context.Background(), result, 0)
	require.NoError(t, err)
	assert.False(t, ParseTasks(context.Background(), result)[0].Completed)
}

// --- AddTaskContent ---

func TestAddTaskContent_IncreasesCount(t *testing.T) {
	result, err := AddTaskContent(context.Background(), tasksNote, "New task")
	require.NoError(t, err)
	assert.Len(t, ParseTasks(context.Background(), result), 4)
}

func TestAddTaskContent_Text(t *testing.T) {
	result, err := AddTaskContent(context.Background(), tasksNote, "My new task")
	require.NoError(t, err)
	assert.Contains(t, taskTexts(ParseTasks(context.Background(), result)), "My new task")
}

func TestAddTaskContent_NewTaskIsIncomplete(t *testing.T) {
	result, err := AddTaskContent(context.Background(), tasksNote, "New task")
	require.NoError(t, err)
	for _, task := range ParseTasks(context.Background(), result) {
		if task.Text == "New task" {
			assert.False(t, task.Completed)
			return
		}
	}
	t.Fatal("new task not found")
}

func TestAddTaskContent_ToEmptySection(t *testing.T) {
	result, err := AddTaskContent(context.Background(), emptyTasksNote, "First task")
	require.NoError(t, err)
	tasks := ParseTasks(context.Background(), result)
	require.Len(t, tasks, 1)
	assert.Equal(t, "First task", tasks[0].Text)
}

func TestAddTaskContent_InvalidFormat(t *testing.T) {
	_, err := AddTaskContent(context.Background(), invalidNote, "Task")
	assert.Error(t, err)
}

func TestAddTaskContent_PreservesExistingTasks(t *testing.T) {
	result, err := AddTaskContent(context.Background(), tasksNote, "Extra")
	require.NoError(t, err)
	texts := taskTexts(ParseTasks(context.Background(), result))
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
