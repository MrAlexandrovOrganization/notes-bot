package resources

import "fmt"

const (
	EnglishMenu             = "🇬🇧 Английский"
	EnglishIntro            = "🇬🇧 Английский\n\nВыбери действие. После выхода повторение продолжится с той же карточки."
	EnglishAdd              = "Добавить в словарь"
	EnglishReview           = "Повторение"
	EnglishBack             = "◀ Назад"
	EnglishSave             = "✅ Сохранить"
	EnglishClarify          = "Уточнить с ИИ"
	EnglishExample          = "Пример с ИИ"
	EnglishDetail           = "Пояснение с ИИ"
	EnglishGenerate         = "Подготовить с ИИ"
	EnglishNew              = "Следующий ввод"
	EnglishPrompt           = "Пришли слово, выражение или фразу с переводом в любом порядке. Например, «put up with — терпеть». Можно разделить пару новой строкой или прислать двумя сообщениями.\n\nДля помощи с ИИ пришли одну сторону. К русскому тексту можно добавить английскую попытку с новой строки после «Мой вариант:»."
	EnglishAwaitTranslation = "Пришли перевод или нажми «Подготовить с ИИ»."
	EnglishPairHelp         = "Для готовой пары пришли русский и английский текст через тире или с новой строки. Каждая сторона может содержать до 600 символов."
	EnglishManualEdit       = "Чтобы исправить карточку, пришли обе стороны заново."
	EnglishClarifyPrompt    = "Пришли уточнение или исправленный текст. Для полной замены начни с «Исправление:». Можно голосом."
	EnglishInvalid          = "Пришли непустой текст до 1000 символов."
	EnglishFailure          = "⏳ Не удалось подготовить вариант. Твой ввод сохранён в черновике. Пришли уточнение или нажми «Повторить»."
	EnglishRetry            = "Повторить"
	EnglishStale            = "Этот шаг уже завершён или карточка изменилась. Открой нужный режим заново."
	EnglishSaved            = "✅ Сохранено в словарь под датой [[%s]]. Можно сразу прислать следующий ввод."
	EnglishInputLabel       = "Твой ввод: "
	EnglishSourceLabel      = "Перевод: "
	EnglishExplanationLabel = "Оборот: "
	EnglishExampleLabel     = "Пример: "
	EnglishContextLabel     = "Контекст: "
	EnglishReviewNoInput    = "Для текста или голоса выбери «Добавить в словарь»."
	EnglishShow             = "Показать ответ"
	EnglishHint             = "Подсказка"
	EnglishMiss             = "Не вспомнил"
	EnglishHard             = "С трудом"
	EnglishEasy             = "Легко"
	EnglishFinish           = "В меню"
	EnglishEmpty            = "Словарь пока пуст. Добавь карточку, чтобы начать повторение."
	EnglishReviewFront      = "Вспомни по-английски:\n\n"
	EnglishHintLabel        = "Подсказка: начало — "
	EnglishVoiceReady       = "🎙 Учебная расшифровка готова. Открой её для просмотра и исправления. В дневник она не записана."
	EnglishVoiceOpen        = "Открыть учебный ввод"
	EnglishVoiceAccepted    = "Голосовой ввод принят для раздела «Английский». Можно продолжать пользоваться ботом; назначение задания сохранено."
	EnglishVoiceTooLong     = "Расшифровка длиннее 1000 символов. Выбери нужный фрагмент и пришли его текстом в раздел «Английский»."
)

const (
	EnglishSkip               = "Пропустить"
	EnglishCorrection         = "Исправление:"
	EnglishUnavailable        = "⏳ Раздел «Английский» пока недоступен. Попробуй позже."
	EnglishExampleInstruction = "Ещё пример: сохрани выбранную английскую формулировку, смысл и пояснение; замени только пример."
	EnglishDetailInstruction  = "Подробнее поясни выбранный оборот, до трёх коротких предложений, сохрани смысл и английскую формулировку."
)

const EnglishTranscriptLabel = "Распознано: "

const EnglishUndated = "Без даты"

const EnglishSaveFailure = "⏳ Не удалось подтвердить сохранение. Нажми «Сохранить» ещё раз: повтор не создаст дубль."
const EnglishGradeFailure = "⏳ Не удалось подтвердить действие. Нажми ту же кнопку ещё раз."

const EnglishPreparing = "⏳ Жду ответ от модели. Можно сразу прислать готовую пару через тире или с новой строки."
const EnglishEnrichmentFailure = "ИИ не смог дополнить карточку. Её по-прежнему можно сохранить."

const EnglishPhraseSourcePrompt = "Пришли саму русскую мысль отдельно, до 600 символов. Английскую попытку можно добавить с новой строки после «Мой вариант:». Этот ввод заменит предыдущий черновик."

func EnglishRoundPosition(round int64, position, total int32) string {
	return fmt.Sprintf("Круг %d · Карточка %d из %d", round, position, total)
}

func EnglishReviewSaved(skipped int32) string {
	switch skipped {
	case 0:
		return "✅ Оценка сохранена. Это направление снова доступно со следующего круга."
	case 1:
		return "✅ Это направление пропустит следующий круг."
	case 2, 3, 4:
		return fmt.Sprintf("✅ Это направление пропустит %d круга.", skipped)
	default:
		return fmt.Sprintf("✅ Это направление пропустит %d кругов.", skipped)
	}
}
