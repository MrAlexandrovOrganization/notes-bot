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
	"required": []string{"title", "schedule_type", "hour", "minute", "days", "day_of_month", "interval_days", "create_task"},
}

const llmSystemPrompt = `Сегодняшняя дата и время: %s

Ты помощник для разбора напоминаний. Из текста пользователя извлеки параметры и верни JSON.

Поля:
- title: короткое название (без слов "напоминай", "каждый" и т.п.)
- schedule_type: ОДНО из значений: "daily", "weekly", "monthly", "yearly", "once", "custom_days"
- hour, minute: время в 24-часовом формате (целые числа)
- days: ОБЯЗАТЕЛЬНО для weekly — массив номеров дней [0=Пн, 1=Вт, 2=Ср, 3=Чт, 4=Пт, 5=Сб, 6=Вс]. Для остальных типов — пустой массив [].
- day_of_month: ОБЯЗАТЕЛЬНО для monthly — число месяца (1–31). Для остальных — 0.
- interval_days: ОБЯЗАТЕЛЬНО для custom_days — интервал в днях. Для остальных — 0.
- month, day: для yearly — месяц (1–12) и число (1–31).
- date: для once — дата в формате YYYY-MM-DD. Если год не указан — используй год из сегодняшней даты; если эта дата уже прошла — следующий год.
- create_task: true только если явно просят создать задачу.

Месяцы: январь=1, февраль=2, март=3, апрель=4, май=5, июнь=6, июль=7, август=8, сентябрь=9, октябрь=10, ноябрь=11, декабрь=12

Примеры:
- "каждый понедельник в 9" → schedule_type="weekly", days=[0], hour=9, minute=0
- "каждый пн и пт в 8:30" → schedule_type="weekly", days=[0,4], hour=8, minute=30
- "по будням в 8 утра" → schedule_type="weekly", days=[0,1,2,3,4], hour=8, minute=0
- "в рабочие дни в 9:00" → schedule_type="weekly", days=[0,1,2,3,4], hour=9, minute=0
- "по выходным в 10" → schedule_type="weekly", days=[5,6], hour=10, minute=0
- "каждую субботу и воскресенье в 11" → schedule_type="weekly", days=[5,6], hour=11, minute=0
- "25 числа каждого месяца в 10" → schedule_type="monthly", day_of_month=25, hour=10, minute=0
- "каждое 20-е число в 15:00" → schedule_type="monthly", day_of_month=20, hour=15, minute=0
- "1-го числа в 9 утра" → schedule_type="monthly", day_of_month=1, hour=9, minute=0
- "каждые 3 дня в 7 утра" → schedule_type="custom_days", interval_days=3, hour=7, minute=0
- "каждый год 2-го июня в 20:00" → schedule_type="yearly", month=6, day=2, hour=20, minute=0
- "каждый год 1 января в 0:00" → schedule_type="yearly", month=1, day=1, hour=0, minute=0
- "каждое 8 марта в 9:00" → schedule_type="yearly", month=3, day=8, hour=9, minute=0
- "ежегодно 15 августа в 12:00" → schedule_type="yearly", month=8, day=15, hour=12, minute=0
- "27-го марта в 19:00" → schedule_type="once", date="<ближайший 27 марта>", hour=19, minute=0
- "15 апреля в 10:00" → schedule_type="once", date="<ближайшее 15 апреля>", hour=10, minute=0
- "завтра в 8:00" → schedule_type="once", date="<сегодня + 1 день>", hour=8, minute=0`

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
