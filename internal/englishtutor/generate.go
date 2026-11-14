package englishtutor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed resources/phrase.txt
var phrasePrompt string

//go:embed resources/word.txt
var wordPrompt string

type Chat func(context.Context, string, string, any, int) (string, error)

type Draft struct {
	Source      string `json:"source"`
	English     string `json:"english"`
	Explanation string `json:"explanation"`
	Example     string `json:"example"`
	Context     string `json:"context"`
	Question    string `json:"question"`
}

type Generator interface {
	PrepareEnglish(context.Context, string, string, string, *Draft) (*Draft, error)
}

func draftSchema(fields ...string) map[string]any {
	properties := map[string]any{}
	for _, field := range fields {
		properties[field] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": properties, "required": fields, "additionalProperties": false}
}

var wordSchema = draftSchema("source", "english", "explanation", "example", "context", "question")
var phraseSchema = draftSchema("english", "explanation", "example", "context", "question")

func Prepare(ctx context.Context, kind, input, instruction string, previous *Draft, chat Chat) (*Draft, error) {
	if (kind != "word" && kind != "phrase") || strings.TrimSpace(input) == "" || !utf8.ValidString(input) || !utf8.ValidString(instruction) || len([]rune(input)) > 1000 || len([]rune(instruction)) > 1000 {
		return nil, fmt.Errorf("invalid English mode")
	}
	if previous != nil {
		for _, value := range []string{previous.Source, previous.English, previous.Explanation, previous.Example, previous.Context, previous.Question} {
			if !utf8.ValidString(value) || len([]rune(value)) > 600 {
				return nil, fmt.Errorf("invalid English preview")
			}
		}
	}
	// Only the selected input and its current preview go to the model. No vault search.
	payload, _ := json.Marshal(struct {
		Kind        string `json:"kind"`
		Input       string `json:"input"`
		Instruction string `json:"instruction"`
		Previous    *Draft `json:"previous"`
	}{kind, input, instruction, previous})
	prompt := phrasePrompt
	schema := phraseSchema
	source := ""
	if kind == "word" {
		prompt = wordPrompt
		schema = wordSchema
	} else {
		var ok bool
		source, ok = RussianThought(input)
		if !ok {
			return nil, fmt.Errorf("separate Russian thought required")
		}
	}
	content, err := chat(ctx, prompt, string(payload), schema, 700)
	if err != nil {
		return nil, err
	}
	var result Draft
	if json.Unmarshal([]byte(content), &result) != nil {
		return nil, fmt.Errorf("invalid English response")
	}
	if kind == "phrase" {
		result.Source = source
	}
	for _, target := range []*string{&result.Source, &result.English, &result.Explanation, &result.Example, &result.Context, &result.Question} {
		*target = strings.TrimSpace(*target)
		if len([]rune(*target)) > 600 || strings.ContainsAny(*target, "\r\n\x00") || strings.Contains(*target, "<!--") {
			return nil, fmt.Errorf("invalid English field")
		}
	}
	if result.Question == "" {
		hasRussianSource := strings.ContainsFunc(result.Source, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) })
		if !hasRussianSource || result.English == "" || result.Example == "" || kind == "phrase" && result.Explanation == "" {
			return nil, fmt.Errorf("incomplete English response")
		}
		if strings.Contains(strings.ToLower(result.Source), strings.ToLower(result.English)) {
			return nil, fmt.Errorf("english answer leaked into cue")
		}
	}
	if strings.Contains(result.English, "`") || strings.Contains(result.Source, " → ") {
		return nil, fmt.Errorf("invalid English delimiters")
	}
	return &result, nil
}
