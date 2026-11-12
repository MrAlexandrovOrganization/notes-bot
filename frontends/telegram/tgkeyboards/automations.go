package tgkeyboards

import (
	"fmt"

	"github.com/mymmrac/telego"

	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/automation/v1"
)

func AutomationCancel() *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.GeoCancel, "automation:list:0"))
}
func AutomationRules(rules []*pb.Rule, page int) *telego.InlineKeyboardMarkup {
	const size = 6
	pages := max(1, (len(rules)+size-1)/size)
	page = max(0, min(page, pages-1))
	var rows [][]telego.InlineKeyboardButton
	for _, r := range rules[page*size : min(len(rules), (page+1)*size)] {
		label := r.TaskTitle
		if !r.Armed {
			label = "✓ " + label
		}
		rows = append(rows, GeoButton(short(label), fmt.Sprintf("automation:view:%d", r.Id)))
	}
	if page > 0 {
		rows = append(rows, GeoButton("◀", fmt.Sprintf("automation:list:%d", page-1)))
	}
	if page+1 < pages {
		rows = append(rows, GeoButton("▶", fmt.Sprintf("automation:list:%d", page+1)))
	}
	rows = append(rows, GeoButton(resources.AutomationNew, "automation:new"), GeoButton(resources.AutomationHistory, "automation:history"), GeoButton(resources.GeoBack, "menu:back"))
	return GeoKeyboard(rows...)
}
func AutomationTasks(tasks []tgstates.AutomationChoice, page int) *telego.InlineKeyboardMarkup {
	const size = 6
	pages := max(1, (len(tasks)+size-1)/size)
	page = max(0, min(page, pages-1))
	var rows [][]telego.InlineKeyboardButton
	for _, t := range tasks[page*size : min(len(tasks), (page+1)*size)] {
		rows = append(rows, GeoButton(short(t.Text), fmt.Sprintf("automation:task:%d", t.Index)))
	}
	if page > 0 {
		rows = append(rows, GeoButton("◀", fmt.Sprintf("automation:tasks:%d", page-1)))
	}
	if page+1 < pages {
		rows = append(rows, GeoButton("▶", fmt.Sprintf("automation:tasks:%d", page+1)))
	}
	rows = append(rows, GeoButton(resources.GeoCancel, "automation:list:0"))
	return GeoKeyboard(rows...)
}
func AutomationStatuses() *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.AutomationCompleted, "automation:status:2"), GeoButton(resources.AutomationRejected, "automation:status:3"), GeoButton(resources.AutomationPending, "automation:status:1"), GeoButton(resources.GeoCancel, "automation:list:0"))
}
func AutomationFrequency() *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.AutomationOnce, "automation:repeat:once"), GeoButton(resources.AutomationRepeat, "automation:repeat:always"), GeoButton(resources.GeoCancel, "automation:list:0"))
}
