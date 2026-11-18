package englishtutor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SplitPair recognizes a pair with an explicit separator. In particular, an
// English example embedded in a Russian sentence is still a single input.
func SplitPair(input string) (russian, english string, ok bool) {
	if attemptLabel.MatchString(input) {
		return "", "", false
	}
	for _, separator := range []string{"\n", " → ", " — ", " – ", " - "} {
		left, right, found := strings.Cut(input, separator)
		if found {
			if russian, english, ok = OrderPair(left, right); ok {
				return russian, english, true
			}
		}
	}
	return "", "", false
}

// OrderPair also accepts two successive messages. Russian text can include
// Latin names, while the English side must have Latin letters and no Cyrillic.
func OrderPair(first, second string) (russian, english string, ok bool) {
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if strings.ContainsFunc(second, isCyrillic) {
		first, second = second, first
	}
	if !strings.ContainsFunc(first, isCyrillic) || strings.ContainsFunc(second, isCyrillic) ||
		!strings.ContainsFunc(second, func(r rune) bool { return unicode.Is(unicode.Latin, r) }) ||
		!validPairSide(first) || !validPairSide(second) || strings.Contains(first, " → ") || strings.Contains(second, "`") {
		return "", "", false
	}
	return first, second, true
}

func isCyrillic(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }

func validPairSide(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= 600 &&
		!strings.ContainsAny(s, "\r\n\x00") && !strings.Contains(s, "<!--")
}
