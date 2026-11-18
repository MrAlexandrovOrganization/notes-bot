package resources

import "fmt"

const (
	EnglishMenu             = "🇬🇧 Английский"
	EnglishIntro            = "🇬🇧 Английский\n\nВыбери действие. Можно закончить в любой момент. Сохраняется только выбранный тобой вариант."
	EnglishWord             = "Добавить слово"
	EnglishPhrase           = "Добавить фразу"
	EnglishReview           = "Повторение"
	EnglishBack             = "◀ Назад"
	EnglishSave             = "✅ Сохранить"
	EnglishClarify          = "Уточнить с ИИ"
	EnglishExample          = "Пример с ИИ"
	EnglishDetail           = "Пояснение с ИИ"
	EnglishGenerate         = "Подготовить с ИИ"
	EnglishNew              = "Следующий ввод"
	EnglishWordPrompt       = "Пришли слово и перевод в любом порядке, например «apple — яблоко». Можно разделить их новой строкой или прислать двумя сообщениями. Для помощи с ИИ пришли слово или выражение, можно с контекстом."
	EnglishPhrasePrompt     = "Пришли фразу и перевод в любом порядке, например «Я уже в пути — I'm on my way». Можно разделить их новой строкой или прислать двумя сообщениями. Для помощи с ИИ пришли мысль по-русски. Если хочешь проверить свою формулировку, добавь её с новой строки после «Мой вариант:»."
	EnglishAwaitTranslation = "Пришли перевод или нажми «Подготовить с ИИ»."
	EnglishPairHelp         = "Для готовой пары пришли русский и английский текст через тире или с новой строки. Каждая сторона может содержать до 600 символов."
	EnglishManualEdit       = "Чтобы исправить карточку, пришли обе стороны заново."
	EnglishClarifyPrompt    = "Пришли уточнение или исправленный текст. Для полной замены начни с «Исправление:». Можно голосом."
	EnglishInvalid          = "Пришли непустой текст до 1000 символов."
	EnglishFailure          = "⏳ Не удалось подготовить вариант. Твой ввод сохранён в черновике. Пришли уточнение или нажми «Повторить»."
	EnglishRetry            = "Повторить"
	EnglishStale            = "Этот шаг уже завершён или карточка изменилась. Открой нужный режим заново."
	EnglishSaved            = "✅ Сохранено в %s под датой [[%s]]. Можно сразу прислать следующий ввод."
	EnglishInputLabel       = "Твой ввод: "
	EnglishSourceLabel      = "Смысл: "
	EnglishExplanationLabel = "Оборот: "
	EnglishExampleLabel     = "Пример: "
	EnglishContextLabel     = "Контекст: "
	EnglishReviewHelp       = "Вспомни про себя или произнеси вслух, затем покажи ответ и оцени себя. Вводить ответ не нужно. За один проход каждая карточка показывается один раз. В конце можно отдельно повторить забытые. Другие правильные формулировки допустимы; оценивай вспоминание выбранного материала."
	EnglishReviewNoInput    = "В этом режиме достаточно кнопок «Показать ответ» и самооценки. Для текста или голоса выбери «Добавить слово» или «Добавить фразу»."
	EnglishWords            = "Слова"
	EnglishPhrases          = "Фразы"
	EnglishAll              = "Всё"
	EnglishShow             = "Показать ответ"
	EnglishHint             = "Подсказка"
	EnglishMiss             = "Не вспомнил"
	EnglishHard             = "С трудом"
	EnglishEasy             = "Легко"
	EnglishNext             = "Следующая"
	EnglishFinish           = "Закончить"
	EnglishEmpty            = "Сейчас нет подошедших карточек. Можно добавить материал или вернуться позже."
	EnglishReviewFront      = "Вспомни по-английски:\n\n"
	EnglishHintLabel        = "Подсказка: начало — "
	EnglishReviewSaved      = "✅ Оценка сохранена. Следующий повтор: %s.\nСерия успехов: %d; интервал: %s; забываний: %d."
	EnglishVoiceReady       = "🎙 Учебная расшифровка готова. Открой её для просмотра и исправления. В дневник она не записана."
	EnglishVoiceOpen        = "Открыть учебный ввод"
	EnglishVoiceAccepted    = "Голосовой ввод принят для раздела «Английский». Можно продолжать пользоваться ботом; назначение задания сохранено."
	EnglishVoiceTooLong     = "Расшифровка длиннее 1000 символов. Выбери нужный фрагмент и пришли его текстом в раздел «Английский»."
)

func EnglishCounts(words, phrases, total, forgotten int) string {
	return fmt.Sprintf("Подошло к повторению: %d (слова: %d, фразы: %d).\nВсего карточек: %d; с повторными забываниями: %d.\n\n%s", words+phrases, words, phrases, total, forgotten, EnglishReviewHelp)
}

const (
	EnglishSkip               = "Пропустить"
	EnglishCorrection         = "Исправление:"
	EnglishUnavailable        = "⏳ Раздел «Английский» пока недоступен. Попробуй позже."
	EnglishExampleInstruction = "Ещё пример: сохрани выбранную английскую формулировку, смысл и пояснение; замени только пример."
	EnglishDetailInstruction  = "Подробнее поясни выбранный оборот, до трёх коротких предложений, сохрани смысл и английскую формулировку."
)

func EnglishInterval(seconds int64) string {
	if seconds < 3600 {
		return fmt.Sprintf("%d мин.", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%d ч.", seconds/3600)
	}
	return fmt.Sprintf("%d дн.", seconds/86400)
}

const EnglishTranscriptLabel = "Распознано: "

const EnglishUndated = "Без даты"

const EnglishSaveFailure = "⏳ Не удалось подтвердить сохранение. Нажми «Сохранить» ещё раз: повтор не создаст дубль."
const EnglishGradeFailure = "⏳ Не удалось подтвердить оценку. Нажми ту же оценку ещё раз: повтор не изменит прогресс дважды."

const EnglishPreparing = "⏳ Жду ответ от модели. Можно сразу прислать готовую пару через тире или с новой строки."
const EnglishEnrichmentFailure = "ИИ не смог дополнить карточку. Её по-прежнему можно сохранить."

const EnglishPhraseSourcePrompt = "Пришли саму русскую мысль отдельно, до 600 символов. Английскую попытку можно добавить с новой строки после «Мой вариант:». Этот ввод заменит предыдущий черновик."

func EnglishPassPosition(current, total int) string {
	return fmt.Sprintf("Карточка %d из %d", current, total)
}
func EnglishRepeatMissed(count int) string {
	return fmt.Sprintf("Повторить забытые (%d)", count)
}
func EnglishPassFinished(seen, missed int) string {
	text := fmt.Sprintf("✅ Проход завершён.\n\nПросмотрено разных карточек: %d.\nНе вспомнили: %d.", seen, missed)
	if missed > 0 {
		text += "\n\nЗабытые карточки можно повторить отдельным проходом."
	}
	return text
}
