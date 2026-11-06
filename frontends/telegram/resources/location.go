package resources

import "fmt"

const LocationSaveError = "❌ Ошибка при сохранении местоположения."

func LocationSaved(date string, live bool) string {
	if live {
		return fmt.Sprintf("📍 Живое местоположение активно — буду записывать каждое обновление к дате %s", date)
	}
	return fmt.Sprintf("📍 Местоположение сохранено к дате %s", date)
}
