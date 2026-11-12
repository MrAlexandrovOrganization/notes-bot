package resources

import "fmt"

const (
	AutomationMenu         = "⚡ Автоматизации"
	AutomationNew          = "➕ После изменения задачи"
	AutomationHistory      = "🕘 История выполнения"
	AutomationUnavailable  = "⏳ Автоматизации пока недоступны."
	AutomationList         = "⚡ Автоматизации\nПравила запускают уведомления при следующем изменении выбранной задачи."
	AutomationChoose       = "Выбери задачу выбранного дня. Дату можно сменить в календаре Notes."
	AutomationNoTasks      = "На выбранную дату нет задач. Сначала добавь задачу или выбери другую дату."
	AutomationStatusPrompt = "Какое изменение должно запустить уведомление?"
	AutomationCompleted    = "✅ Выполнена"
	AutomationPending      = "❓ Возвращена к выполнению"
	AutomationRejected     = "❌ Отклонена"
	AutomationTextPrompt   = "О чём напомнить после этого изменения? До 500 символов."
	AutomationDelayPrompt  = "Через сколько минут отправить уведомление? 0 — сразу, максимум 43200 минут (30 дней)."
	AutomationRepeatPrompt = "Сколько раз запускать правило?"
	AutomationOnce         = "Один раз"
	AutomationRepeat       = "При каждом таком изменении"
	AutomationDeletePrompt = "Удалить правило и отменить его ожидающие уведомления?"
	AutomationNoHistory    = "Правила пока не срабатывали."
	AutomationChanged      = "Задача изменилась. Выбери её заново."
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
func AutomationConfirm(title, date string, state, minutes int32, text string, repeat bool) string {
	frequency := AutomationOnce
	if repeat {
		frequency = AutomationRepeat
	}
	return fmt.Sprintf("⚡ Когда задача «%s» (%s) будет %s\n\nУведомление: %s\nЧерез: %d мин.\nПовторение: %s\n\nТекущее состояние задачи не запускает правило — ожидается следующее изменение.", title, date, AutomationStatus(state), text, minutes, frequency)
}
func AutomationExecution(title string, state string, attempts int32, due string) string {
	label := map[string]string{"pending": "ожидает отправки", "sending": "отправляется", "sent": "отправлено", "expired": "срок истёк", "cancelled": "отменено"}[state]
	r := []rune(title)
	if len(r) > 40 {
		title = string(r[:40]) + "…"
	}
	return fmt.Sprintf("• %s · %s — %s, попыток: %d", due, title, label, attempts)
}
func AutomationNotice(text string) string {
	return "⚡ Напоминание после изменения задачи:\n\n" + text
}
