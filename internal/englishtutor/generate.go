package englishtutor

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

//go:embed resources/prompt.txt
var systemPrompt string

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

var englishSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"source": map[string]any{"type": "string"}, "english": map[string]any{"type": "string"},
		"explanation": map[string]any{"type": "string"}, "example": map[string]any{"type": "string"},
		"context": map[string]any{"type": "string"}, "question": map[string]any{"type": "string"},
	},
	"required":             []string{"source", "english", "explanation", "example", "context", "question"},
	"additionalProperties": false,
}

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
		Kind, Input, Instruction string
		Previous                 *Draft
	}{kind, input, instruction, previous})
	content, err := chat(ctx, systemPrompt, string(payload), englishSchema, 700)
	if err != nil {
		return nil, err
	}
	var result Draft
	if json.Unmarshal([]byte(content), &result) != nil {
		return nil, fmt.Errorf("invalid English response")
	}
	for _, target := range []*string{&result.Source, &result.English, &result.Explanation, &result.Example, &result.Context, &result.Question} {
		*target = strings.TrimSpace(*target)
		if len([]rune(*target)) > 600 || strings.ContainsAny(*target, "\r\n\x00") || strings.Contains(*target, "<!--") {
			return nil, fmt.Errorf("invalid English field")
		}
	}
	if result.Question == "" && (result.Source == "" || result.English == "" || result.Example == "") {
		return nil, fmt.Errorf("incomplete English response")
	}
	if strings.Contains(result.English, "`") || strings.Contains(result.Source, " → ") {
		return nil, fmt.Errorf("invalid English delimiters")
	}
	return &result, nil
}
