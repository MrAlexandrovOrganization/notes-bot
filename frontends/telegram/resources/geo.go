package resources

import "fmt"

const (
	GeoMenu              = "📍 Места и события"
	GeoUnavailable       = "⏳ Напоминания по месту пока недоступны."
	GeoPlaces            = "📍 Места\n\nВыбери место или добавь новое. Для своевременных напоминаний включи немедленную отправку в Locations."
	GeoNewPlace          = "➕ Добавить место"
	GeoRules             = "🔔 Напоминания по месту"
	GeoBack              = "◀ Назад"
	GeoCancel            = "❌ Отмена"
	GeoSave              = "✅ Сохранить"
	GeoDelete            = "🗑 Удалить"
	GeoEdit              = "✏️ Изменить место"
	GeoEntered           = "🚶 Когда приду"
	GeoExited            = "🚪 Когда уйду"
	GeoOnce              = "Один раз"
	GeoAlways            = "Каждый раз"
	GeoDaily             = "Максимум раз в день"
	GeoNamePrompt        = "Как назвать место? Например: Дом, Работа, Друзья. До 80 символов."
	GeoPointPrompt       = "Отправь точку на карте через скрепку → Геопозиция (обычную, не трансляцию), либо координаты: широта долгота."
	GeoRadiusPrompt      = "Введи радиус места в метрах: от 100 до 10000. Например, 150."
	GeoTitlePrompt       = "О чём напомнить? До 500 символов."
	GeoRepeatPrompt      = "Как часто напоминать?"
	GeoInvalid           = "Не удалось принять значение. Проверь формат и попробуй ещё раз."
	GeoSaved             = "✅ Сохранено."
	GeoStale             = "Этот шаг уже завершён. Открой раздел заново."
	GeoDeletePlacePrompt = "Удалить место и отключить все связанные напоминания?"
	GeoDeleteRulePrompt  = "Удалить это напоминание?"
	GeoRulesEmpty        = "Напоминаний по месту пока нет. Открой место и выбери «Когда приду» или «Когда уйду»."
	GeoRulesHeader       = "🔔 Напоминания по месту"
)

func GeoState(state string) string {
	switch state {
	case "inside":
		return "внутри"
	case "outside":
		return "снаружи"
	default:
		return "нет свежего подтверждения"
	}
}
func GeoTransition(event string) string {
	if event == "exited" {
		return "когда уйду"
	}
	return "когда приду"
}
func GeoRepetition(repeat string) string {
	switch repeat {
	case "once":
		return GeoOnce
	case "daily":
		return GeoDaily
	default:
		return GeoAlways
	}
}
func GeoPlaceCard(name string, radius float64, seconds int32, state string) string {
	return fmt.Sprintf("📍 %s\nРадиус: %.0f м\nПодтверждение перехода: %d с\nСостояние: %s", name, radius, seconds, GeoState(state))
}
func GeoPlaceConfirm(name string, radius float64) string {
	return fmt.Sprintf("Сохранить место «%s» с радиусом %.0f м?\nВход и выход подтверждаются свежими точками в течение минуты. Первая позиция задаёт исходное состояние без уведомления.", name, radius)
}
func GeoRuleConfirm(title, event, repeat string) string {
	return fmt.Sprintf("🔔 %s\nСобытие: %s\nПовторение: %s\nОжидается следующий переход; старые события не запускают правило. Срок доставки после подтверждения: 5 минут.", title, GeoTransition(event), GeoRepetition(repeat))
}
func GeoRuleLabel(title string, armed bool) string {
	if !armed {
		return "✓ " + title
	}
	return title
}
func GeoNotification(title, place, event string) string {
	return fmt.Sprintf("🔔 %s\n\n📍 %s — %s", title, place, GeoTransition(event))
}

func GeoRuleCard(title, place, event, repeat string, armed bool) string {
	state := "ожидает событие"
	if !armed {
		state = "уже сработало"
	}
	return fmt.Sprintf("🔔 %s\nМесто: %s\nСобытие: %s\nПовторение: %s\nСостояние: %s", title, place, GeoTransition(event), GeoRepetition(repeat), state)
}
