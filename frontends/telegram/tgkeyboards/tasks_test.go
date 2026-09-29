package tgkeyboards

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/notes"
)

func makeTasks(n int) []*clients.Task {
	tasks := make([]*clients.Task, n)
	for i := range tasks {
		tasks[i] = &clients.Task{Text: fmt.Sprintf("Task %d", i+1), Index: i, State: pb.TaskState_TASK_STATE_PENDING}
	}
	return tasks
}

func makeCompletedTask() *clients.Task {
	return &clients.Task{Text: "Done task", Index: 0, State: pb.TaskState_TASK_STATE_COMPLETED}
}

func makeRejectedTask() *clients.Task {
	return &clients.Task{Text: "Rejected task", Index: 0, State: pb.TaskState_TASK_STATE_INCOMPLETE}
}

func TestTasks_Empty(t *testing.T) {
	kb := Tasks(nil, 0)
	rows := kb.InlineKeyboard
	require.GreaterOrEqual(t, len(rows), 2)
	found := false
	for _, row := range rows {
		for _, btn := range row {
			if btn.Text == "➕ Добавить задачу" {
				found = true
			}
		}
	}
	assert.True(t, found, "Add button should be present for empty list")
}

func TestTasks_FewTasks_NoPagination(t *testing.T) {
	kb := Tasks(makeTasks(3), 0)
	rows := kb.InlineKeyboard
	// 3 task rows + add row + back row = 5 rows
	assert.Equal(t, 5, len(rows))
}

func TestTasks_PendingTask_ShowsQuestionMark(t *testing.T) {
	tasks := []*clients.Task{
		{Text: "Buy milk", Index: 2, State: pb.TaskState_TASK_STATE_PENDING},
	}
	kb := Tasks(tasks, 0)
	btn := kb.InlineKeyboard[0][0]
	require.NotEmpty(t, btn.CallbackData)
	assert.Equal(t, "task:toggle:2", btn.CallbackData)
	assert.Contains(t, btn.Text, "Buy milk")
	assert.Contains(t, btn.Text, "❓")
}

func TestTasks_CompletedTask_ShowsCheckmark(t *testing.T) {
	tasks := []*clients.Task{
		{Text: "Done task", Index: 0, State: pb.TaskState_TASK_STATE_COMPLETED},
	}
	kb := Tasks(tasks, 0)
	btn := kb.InlineKeyboard[0][0]
	assert.Contains(t, btn.Text, "✅")
}

func TestTasks_RejectedTask_ShowsX(t *testing.T) {
	tasks := []*clients.Task{
		{Text: "Rejected task", Index: 0, State: pb.TaskState_TASK_STATE_INCOMPLETE},
	}
	kb := Tasks(tasks, 0)
	btn := kb.InlineKeyboard[0][0]
	assert.Contains(t, btn.Text, "❌")
}

func TestTasks_Pagination_FirstPage(t *testing.T) {
	kb := Tasks(makeTasks(7), 0)
	rows := kb.InlineKeyboard
	// 5 task rows + add row + pagination row + back row = 8 rows
	assert.Equal(t, 8, len(rows))

	navRow := rows[len(rows)-2] // pagination row is second-to-last
	texts := make([]string, len(navRow))
	for i, btn := range navRow {
		texts[i] = btn.Text
	}
	assert.NotContains(t, texts, "◀", "no prev on first page")
	assert.Contains(t, texts, "▶")
	found := false
	for _, t2 := range texts {
		if t2 == "1/2" {
			found = true
		}
	}
	assert.True(t, found, "page indicator 1/2 expected")
}

func TestTasks_Pagination_LastPage(t *testing.T) {
	kb := Tasks(makeTasks(7), 1)
	rows := kb.InlineKeyboard
	// 2 task rows + add row + pagination row + back row = 5 rows
	assert.Equal(t, 5, len(rows))

	navRow := rows[len(rows)-2]
	texts := make([]string, len(navRow))
	for i, btn := range navRow {
		texts[i] = btn.Text
	}
	assert.Contains(t, texts, "◀")
	assert.NotContains(t, texts, "▶", "no next on last page")
}

func TestTasks_BackButtonCallback(t *testing.T) {
	kb := Tasks(nil, 0)
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	require.Len(t, lastRow, 1)
	require.NotEmpty(t, lastRow[0].CallbackData)
	assert.Equal(t, "task:back", lastRow[0].CallbackData)
}

func TestTaskAdd_CancelButton(t *testing.T) {
	kb := TaskAdd()
	require.Len(t, kb.InlineKeyboard, 2)
	require.Len(t, kb.InlineKeyboard[0], 1)
	require.NotEmpty(t, kb.InlineKeyboard[0][0].CallbackData)
	assert.Equal(t, "task:cancel", kb.InlineKeyboard[0][0].CallbackData)
	assert.Equal(t, "task:cancel", kb.InlineKeyboard[1][0].CallbackData)
}
