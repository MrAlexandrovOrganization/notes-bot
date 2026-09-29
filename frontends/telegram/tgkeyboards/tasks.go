package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/notes"
)

const tasksPerPage = 5

func Tasks(tasks []*clients.Task, currentPage int) telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton

	totalPages := (len(tasks) + tasksPerPage - 1) / tasksPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	startIdx := currentPage * tasksPerPage
	endIdx := startIdx + tasksPerPage
	if endIdx > len(tasks) {
		endIdx = len(tasks)
	}

	for _, task := range tasks[startIdx:endIdx] {
		var icon string
		switch task.State {
		case pb.TaskState_TASK_STATE_COMPLETED:
			icon = "✅"
		case pb.TaskState_TASK_STATE_INCOMPLETE:
			icon = "❌"
		default:
			icon = "❓"
		}
		label := fmt.Sprintf("%s %s", icon, task.Text)
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("task:toggle:%d", task.Index)),
		))
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("➕ Добавить задачу").WithCallbackData("task:add"),
	))

	if totalPages > 1 {
		var nav []telego.InlineKeyboardButton
		if currentPage > 0 {
			nav = append(nav, tu.InlineKeyboardButton("◀").WithCallbackData(fmt.Sprintf("task:page:%d", currentPage-1)))
		}
		nav = append(nav, tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", currentPage+1, totalPages)).WithCallbackData("task:noop"))
		if currentPage < totalPages-1 {
			nav = append(nav, tu.InlineKeyboardButton("▶").WithCallbackData(fmt.Sprintf("task:page:%d", currentPage+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("◀ Назад").WithCallbackData("task:back"),
	))

	return *tu.InlineKeyboard(rows...)
}

func TaskAdd() telego.InlineKeyboardMarkup {
	return *tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Отмена").WithCallbackData("task:cancel"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("◀ Назад").WithCallbackData("task:cancel"),
		),
	)
}
