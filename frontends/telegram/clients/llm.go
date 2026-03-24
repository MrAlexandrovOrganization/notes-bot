package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrLLMUnavailable is returned when the Ollama service is unreachable or returns an error.
var ErrLLMUnavailable = errors.New("LLM service unavailable")

// LLMService parses a natural-language reminder description.
type LLMService interface {
	ParseReminder(ctx context.Context, text, currentDate string) (*LLMReminderResult, error)
}

// LLMReminderResult holds the structured reminder data extracted by the LLM.
type LLMReminderResult struct {
	Title        string `json:"title"`
	ScheduleType string `json:"schedule_type"` // daily/weekly/monthly/yearly/once/custom_days
	Hour         int    `json:"hour"`
	Minute       int    `json:"minute"`
	Days         []int  `json:"days"`          // weekly: 0=Mon…6=Sun
	DayOfMonth   int    `json:"day_of_month"`  // monthly
	Month        int    `json:"month"`         // yearly
	Day          int    `json:"day"`           // yearly
	Date         string `json:"date"`          // once: YYYY-MM-DD
	IntervalDays int    `json:"interval_days"` // custom_days
	CreateTask   bool   `json:"create_task"`
}

// LLMClient is an HTTP client for the Ollama /api/chat endpoint.
type LLMClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewLLMClient creates a new LLMClient targeting the given Ollama host/port with the specified model.
func NewLLMClient(host, port, model string) *LLMClient {
	return &LLMClient{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   any             `json:"format"`
}

type ollamaChatResponse struct {
	Message ollamaMessage `json:"message"`
}

// llmSchema is the JSON Schema passed to Ollama's structured output feature.
var llmSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"title":         map[string]any{"type": "string"},
		"schedule_type": map[string]any{"type": "string", "enum": []string{"daily", "weekly", "monthly", "yearly", "once", "custom_days"}},
		"hour":          map[string]any{"type": "integer", "minimum": 0, "maximum": 23},
		"minute":        map[string]any{"type": "integer", "minimum": 0, "maximum": 59},
		"days":          map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
		"day_of_month":  map[string]any{"type": "integer", "minimum": 1, "maximum": 31},
		"month":         map[string]any{"type": "integer", "minimum": 1, "maximum": 12},
		"day":           map[string]any{"type": "integer", "minimum": 1, "maximum": 31},
		"date":          map[string]any{"type": "string"},
		"interval_days": map[string]any{"type": "integer", "minimum": 1},
		"create_task":   map[string]any{"type": "boolean"},
	},
	"required": []string{"title", "schedule_type", "hour", "minute", "create_task"},
}

const llmSystemPrompt = `Ты помощник для разбора напоминаний. Из текста пользователя извлеки параметры напоминания и верни JSON.

Правила:
- title: короткое название напоминания (без слов "напоминай", "каждый" и т.п.)
- schedule_type: тип расписания:
  - "daily" — каждый день
  - "weekly" — по дням недели (заполни days: 0=Пн, 1=Вт, 2=Ср, 3=Чт, 4=Пт, 5=Сб, 6=Вс)
  - "monthly" — каждый месяц (заполни day_of_month: 1–31)
  - "yearly" — раз в год (заполни month и day)
  - "once" — один раз (заполни date в формате YYYY-MM-DD)
  - "custom_days" — каждые N дней (заполни interval_days)
- hour, minute: время срабатывания в 24-часовом формате
- create_task: true если явно просят создать задачу

Сегодняшняя дата: %s`

// ParseReminder calls Ollama with structured output to parse a natural-language reminder description.
func (c *LLMClient) ParseReminder(ctx context.Context, text, currentDate string) (*LLMReminderResult, error) {
	reqBody := ollamaChatRequest{
		Model: c.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: fmt.Sprintf(llmSystemPrompt, currentDate)},
			{Role: "user", Content: text},
		},
		Stream: false,
		Format: llmSchema,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrLLMUnavailable, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrLLMUnavailable, resp.StatusCode)
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var result LLMReminderResult
	if err := json.Unmarshal([]byte(ollamaResp.Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parse LLM result: %w", err)
	}

	return &result, nil
}
