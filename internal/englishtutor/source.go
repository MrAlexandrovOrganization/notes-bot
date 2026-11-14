package englishtutor

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var attemptLabel = regexp.MustCompile(`(?i)(?:^|\s)(?:мой вариант|моя попытка|английская попытка|по-английски|my attempt|my version|english)\s*:`)
var unlabelledAttempt = regexp.MustCompile(`[A-Za-z]+(?:\s+[a-z][a-z']+){2,}`)

// RussianThought keeps the user's wording, not the model's reconstruction.
// An optional attempt follows a label or occupies a separate English-only line.
// Ambiguous inline mixtures ask for a separate Russian thought instead of
// accidentally putting an English answer on the front of a review card.
func RussianThought(input string) (string, bool) {
	if !utf8.ValidString(input) {
		return "", false
	}
	if label := attemptLabel.FindStringIndex(input); label != nil {
		input = input[:label[0]]
	}
	var lines []string
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsFunc(line, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
			lines = append(lines, line)
		} else if len(lines) > 0 {
			break
		}
	}
	source := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if source == "" || len([]rune(source)) > 600 || unlabelledAttempt.MatchString(source) {
		return "", false
	}
	return source, true
}

var correction = regexp.MustCompile(`(?is)^исправление\s*[:.—-]\s*(.*)$`)

// CorrectedInput accepts the punctuation commonly produced by transcription,
// while leaving a normal thought such as «Исправление заняло час» untouched.
func CorrectedInput(input string) (string, bool) {
	match := correction.FindStringSubmatch(strings.TrimSpace(input))
	if match == nil {
		return "", false
	}
	return strings.TrimSpace(match[1]), true
}
