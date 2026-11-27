package resources

import (
	"fmt"

	"notes-bot/internal/taskactions"
)

const (
	AutomationMenu            = "⚡ Автоматизации"
	AutomationNew             = "➕ После изменения задачи"
	AutomationHistory         = "🕘 История выполнения"
	AutomationUnavailable     = "⏳ Автоматизации пока недоступны."
	AutomationList            = "⚡ Автоматизации\nПравила выполняют действия при следующем изменении выбранной задачи."
	AutomationChoose          = "Выбери задачу выбранного дня. Дату можно сменить в календаре Notes."
	AutomationNoTasks         = "На выбранную дату нет задач. Сначала добавь задачу или выбери другую дату."
	AutomationStatusPrompt    = "Какое изменение должно запустить автоматизацию?"
	AutomationCompleted       = "✅ Выполнена"
	AutomationPending         = "❓ Возвращена к выполнению"
	AutomationRejected        = "❌ Отклонена"
	AutomationTextPrompt      = "Какой текст прислать в уведомлении? До 500 символов."
	AutomationDelayPrompt     = "Сколько минут подождать перед действием? 0 — сразу, максимум 43200 минут (30 дней)."
	AutomationRepeatPrompt    = "Сколько раз запускать правило?"
	AutomationOnce            = "Один раз"
	AutomationRepeat          = "При каждом таком изменении"
	AutomationDeletePrompt    = "Удалить правило и отменить его ожидающие действия?"
	AutomationNoHistory       = "Правила пока не срабатывали."
	AutomationChanged         = "Задача изменилась. Выбери её заново."
	AutomationActionPrompt    = "Что сделать после изменения задачи?"
	AutomationNoActions       = "Укажи текст уведомления, название задачи или оба значения."
	AutomationNotify          = "Уведомить"
	AutomationCreateTask      = "Создать задачу"
	AutomationBoth            = "Уведомить и создать задачу"
	AutomationTaskTextPrompt  = "Как назвать задачу? До 500 символов в одной строке."
	AutomationCancelExecution = "Отменить действие"
	AutomationAlreadyStarted  = "Действие уже обработано или начало выполняться."
	CreateTaskEnabled         = "☑️ Создавать задачу"
	CreateTaskDisabled        = "⬜ Создавать задачу"
)

func AutomationStatus(state int32) string {
	switch state {
	case 2:
		return "выполнена"
	case 3:
		return "отклонена"
	default:
		return "возвращена к выполнению"
	}
}
func AutomationConfirm(title, date string, state, minutes int32, notification, task string, repeat bool) string {
	frequency := AutomationOnce
	if repeat {
		frequency = AutomationRepeat
	}
	actions := ""
	if notification != "" {
		actions += "\n" + AutomationNotify + " — " + notification
	}
	if task != "" {
		actions += "\n" + AutomationCreateTask + " — " + task
	}
	return fmt.Sprintf("⚡ Когда задача «%s» (%s) будет %s\n\nОжидание — %d мин.%s\nПовторение — %s", title, date, AutomationStatus(state), minutes, actions, frequency)
}
func AutomationExecution(title string, state, kind string, attempts int32, due string) string {
	label := map[string]string{"pending": "запланировано", "sending": "выполняется", "sent": "выполнено", "expired": "срок истёк", "cancelled": "отменено"}[state]
	action := AutomationNotify
	if kind == taskactions.CreateTask {
		action = AutomationCreateTask
	}
	r := []rune(title)
	if len(r) > 40 {
		title = string(r[:40]) + "…"
	}
	return fmt.Sprintf("• %s · %s · %s — %s, попыток %d", due, title, action, label, attempts)
}
func AutomationNotice(text string) string {
	return "⚡ Напоминание после изменения задачи:\n\n" + text
}
