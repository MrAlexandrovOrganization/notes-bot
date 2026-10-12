package resources

import (
	"fmt"
	"math"
)

const (
	VoiceReceiving       = "⏳ Принимаю голосовое..."
	VoiceAcceptanceRetry = "⚠️ Пока не удалось сохранить голосовое. Повторю приём автоматически."
	VoiceAccepted        = "⏳ Голосовое сохранено. Расшифрую, когда будет доступен воркер; можно продолжать работу."
	VoiceContinue        = "Голосовое принято, можешь продолжать работу."
	VoiceDownloading     = "⏳ Скачиваю аудио..."
	VoiceSubmitting      = "⏳ Отправляю на расшифровку..."
	VoiceUploading       = "⏳ Загружаю аудио в сервис..."
	VoiceQueued          = "⏳ В очереди. Ожидаю доступный воркер..."
	VoiceRunning         = "⏳ Расшифровываю..."
	VoiceCancelled       = "❌ Отменено."
	VoiceFailed          = "❌ Не удалось расшифровать аудио. Отправьте его ещё раз."
	VoiceEmpty           = "⚠️ Не удалось распознать речь."
	VoiceSaved           = "🎙 Добавлено в заметку:"
	VoiceResult          = "🎙 Расшифровка сохранена:"
	VoiceOpen            = "Открыть результат"
	VoiceReminderPreview = "⏰ Подготовлено напоминание. Откройте результат для проверки перед созданием."
	VoiceSmartPreview    = "✨ Запрос обработан. Откройте результат для проверки действия."
)

func VoiceProgress(percent float32) string {
	if percent <= 0 || math.IsNaN(float64(percent)) || math.IsInf(float64(percent), 0) {
		return VoiceRunning
	}
	return fmt.Sprintf("%s %.0f%%", VoiceRunning, min(percent, 100))
}
