package resources

import "fmt"

const (
	EnglishMenu             = "🇬🇧 Английский"
	EnglishIntro            = "🇬🇧 Английский\n\nВыбери действие. Можно закончить в любой момент. Сохраняется только выбранный тобой вариант."
	EnglishWord             = "Добавить слово"
	EnglishPhrase           = "Как выразить мысль"
	EnglishReview           = "Повторение"
	EnglishBack             = "◀ Назад"
	EnglishSave             = "✅ Сохранить"
	EnglishClarify          = "Уточнить"
	EnglishExample          = "Ещё пример"
	EnglishDetail           = "Подробнее"
	EnglishNew              = "Следующий ввод"
	EnglishWordPrompt       = "Пришли английское слово или выражение, лучше с фразой, где оно встретилось. Можно текстом или голосом. Выберем одно значение в контексте."
	EnglishPhrasePrompt     = "Какую мысль хочешь выразить по-английски? Пришли её по-русски текстом или голосом. Свою английскую попытку можно добавить с новой строки после «Мой вариант:», но это необязательно."
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
	EnglishReviewHelp       = "Вспомни про себя или произнеси вслух, затем покажи ответ и оцени себя. Вводить ответ не нужно. Другие правильные формулировки допустимы; оценивай вспоминание выбранного материала."
	EnglishReviewNoInput    = "В этом режиме достаточно кнопок: «Показать ответ» и самооценка. Для текста или голоса выбери «Добавить слово» или «Как выразить мысль»."
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

const EnglishPreparing = "⏳ Готовлю вариант. При первом запуске модель может загружаться несколько минут."

const EnglishPhraseSourcePrompt = "Пришли саму русскую мысль отдельно, до 600 символов. Английскую попытку можно добавить с новой строки после «Мой вариант:». Этот ввод заменит предыдущий черновик."
