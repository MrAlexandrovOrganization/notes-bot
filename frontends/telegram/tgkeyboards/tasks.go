package tgkeyboards

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"notes_bot/frontends/telegram/clients"
)

const tasksPerPage = 5

func Tasks(tasks []*clients.Task, currentPage int) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

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
		checkbox := "❌"
		if task.Completed {
			checkbox = "✅"
		}
		label := fmt.Sprintf("%s %s", checkbox, task.Text)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("task:toggle:%d", task.Index)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить задачу", "task:add"),
	))

	if totalPages > 1 {
		var nav []tgbotapi.InlineKeyboardButton
		if currentPage > 0 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀", fmt.Sprintf("task:page:%d", currentPage-1)))
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", currentPage+1, totalPages), "task:noop"))
		if currentPage < totalPages-1 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("▶", fmt.Sprintf("task:page:%d", currentPage+1)))
		}
		rows = append(rows, nav)
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("◀ Назад", "task:back"),
	))

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func TaskAdd() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "task:cancel"),
		),
	)
}
