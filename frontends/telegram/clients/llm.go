package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ErrLLMUnavailable is returned when the Ollama service is unreachable or returns an error.
var ErrLLMUnavailable = errors.New("LLM service unavailable")

// LLMService parses a natural-language reminder description and classifies
// arbitrary user input into a single high-level intent (note/task/reminder).
//
// today/tomorrow/dayAfter передаются в формате YYYY-MM-DD и должны быть посчитаны
// вызывающим с учётом DAY_START_HOUR (через timeutil.LogicalToday).
type LLMService interface {
	ParseReminder(ctx context.Context, text, currentDateTime, today, tomorrow, dayAfter string) (*LLMReminderResult, error)
	ClassifyIntent(ctx context.Context, text, currentDateTime string) (*LLMIntentResult, error)
}

// SmartIntent enumerates the high-level actions the smart router can produce.
const (
	IntentNote     = "note"
	IntentTask     = "task"
	IntentReminder = "reminder"
	IntentUnknown  = "unknown"
)

// LLMIntentResult — результат классификации произвольного сообщения пользователя.
// Распарсенные параметры напоминания (для intent=reminder) забираются отдельным
// вызовом ParseReminder — так каждый запрос остаётся коротким и стабильным.
type LLMIntentResult struct {
	Intent     string  `json:"intent"`     // "note"|"task"|"reminder"|"unknown"
	Confidence float64 `json:"confidence"` // 0..1
	Title      string  `json:"title"`      // короткий текст для task (для note берём исходник)
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
		http:    &http.Client{Timeout: 3 * time.Minute},
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
	// Think turns reasoning on/off for hybrid-thinking models (qwen3, qwen3.5,
	// deepseek-r1 и т.п.). Указатель — чтобы отличить «не задано» от false.
	Think   *bool          `json:"think,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

// thinkTagRegexp вырезает блоки <think>…</think> на случай, если модель проигнорирует think=false.
var thinkTagRegexp = regexp.MustCompile(`(?is)<think>.*?</think>`)

// extractJSON достаёт первый сбалансированный JSON-объект из ответа модели:
// qwen3.5 иногда заворачивает ответ в ```json … ``` или добавляет текст вокруг,
// игнорируя format-schema.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return s
	}
	return s[start : end+1]
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
	"required": []string{"title", "schedule_type", "hour", "minute", "days", "day_of_month", "interval_days", "date", "create_task"},
}

// llmSystemPrompt принимает 4 аргумента:
//
//	currentDateTime — сейчас "YYYY-MM-DD HH:MM"
//	today           — логическое сегодня "YYYY-MM-DD"
//	tomorrow        — логическое завтра  "YYYY-MM-DD"
//	dayAfter        — послезавтра        "YYYY-MM-DD"
const llmSystemPrompt = `Сейчас: %s
Сегодня (для слов "сегодня", "сегодня вечером"): %s
Завтра (для слов "завтра"): %s
Послезавтра (для слов "послезавтра"): %s

Ты помощник для разбора напоминаний. Из текста пользователя извлеки параметры и верни JSON.

Поля:
- title: короткое название (без слов "напоминай", "каждый" и т.п.)
- schedule_type: ОДНО из значений: "daily", "weekly", "monthly", "yearly", "once", "custom_days"
- hour, minute: время в 24-часовом формате (целые числа)
- days: ОБЯЗАТЕЛЬНО для weekly — массив номеров дней [0=Пн, 1=Вт, 2=Ср, 3=Чт, 4=Пт, 5=Сб, 6=Вс]. Для остальных типов — пустой массив [].
- day_of_month: ОБЯЗАТЕЛЬНО для monthly — число месяца (1–31). Для остальных — 0.
- interval_days: ОБЯЗАТЕЛЬНО для custom_days — интервал в днях. Для остальных — 0.
- month, day: для yearly — месяц (1–12) и число (1–31).
- date: для once — РЕАЛЬНАЯ дата в формате YYYY-MM-DD (например 2026-06-29). Никогда не возвращай шаблоны типа "<завтра>" или "<сегодня + 1>".
  Если пользователь сказал "завтра" — подставь значение поля Завтра выше дословно. Если "послезавтра" — Послезавтра.
  Если назван месяц без года — используй ближайшую будущую дату (год берётся из Сегодня).
  Дата ВСЕГДА должна быть >= Сегодня. Если получается прошлая дата — увеличь год на 1.
- create_task: true только если явно просят создать задачу.

Месяцы: январь=1, февраль=2, март=3, апрель=4, май=5, июнь=6, июль=7, август=8, сентябрь=9, октябрь=10, ноябрь=11, декабрь=12

Примеры (для контекста Сегодня=%[2]s, Завтра=%[3]s, Послезавтра=%[4]s):
- "каждый понедельник в 9" → schedule_type="weekly", days=[0], hour=9, minute=0
- "каждый пн и пт в 8:30" → schedule_type="weekly", days=[0,4], hour=8, minute=30
- "по будням в 8 утра" → schedule_type="weekly", days=[0,1,2,3,4], hour=8, minute=0
- "по выходным в 10" → schedule_type="weekly", days=[5,6], hour=10, minute=0
- "25 числа каждого месяца в 10" → schedule_type="monthly", day_of_month=25, hour=10, minute=0
- "каждое 20-е число в 15:00" → schedule_type="monthly", day_of_month=20, hour=15, minute=0
- "каждые 3 дня в 7 утра" → schedule_type="custom_days", interval_days=3, hour=7, minute=0
- "каждый год 2-го июня в 20:00" → schedule_type="yearly", month=6, day=2, hour=20, minute=0
- "каждое 8 марта в 9:00" → schedule_type="yearly", month=3, day=8, hour=9, minute=0
- "завтра в 8:00" → schedule_type="once", date="%[3]s", hour=8, minute=0
- "послезавтра в 14:00" → schedule_type="once", date="%[4]s", hour=14, minute=0
- "сегодня в 23:00" → schedule_type="once", date="%[2]s", hour=23, minute=0`

// chat выполняет POST /api/chat с отключённым reasoning и компактными options.
// Возвращает уже очищенное содержимое (без <think> и без markdown-обёртки).
func (c *LLMClient) chat(ctx context.Context, system, user string, schema any, numPredict int) (string, error) {
	thinkOff := false
	reqBody := ollamaChatRequest{
		Model: c.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
		Format: schema,
		Think:  &thinkOff,
		Options: map[string]any{
			"num_predict": numPredict,
			"temperature": 0,
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrLLMUnavailable, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: status %d", ErrLLMUnavailable, resp.StatusCode)
	}

	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	content := strings.TrimSpace(thinkTagRegexp.ReplaceAllString(ollamaResp.Message.Content, ""))
	return extractJSON(content), nil
}

// ParseReminder calls Ollama with structured output to parse a natural-language reminder description.
func (c *LLMClient) ParseReminder(ctx context.Context, text, currentDateTime, today, tomorrow, dayAfter string) (*LLMReminderResult, error) {
	content, err := c.chat(ctx, fmt.Sprintf(llmSystemPrompt, currentDateTime, today, tomorrow, dayAfter), text, llmSchema, 256)
	if err != nil {
		return nil, err
	}

	var result LLMReminderResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse LLM result: %w", err)
	}

	return &result, nil
}

// intentSchema — плоская JSON Schema для классификации намерения.
var intentSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"intent":     map[string]any{"type": "string", "enum": []string{IntentNote, IntentTask, IntentReminder, IntentUnknown}},
		"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"title":      map[string]any{"type": "string"},
	},
	"required": []string{"intent", "confidence", "title"},
}

const intentSystemPrompt = `Сегодняшняя дата и время: %s

Ты классификатор сообщений пользователя. Верни ровно один JSON-объект.

Поля:
- intent: одно из "note", "task", "reminder", "unknown".
- confidence: число от 0 до 1, твоя уверенность.
- title: короткий заголовок (обязательно строка, может быть пустой строкой "" если не применимо).

Как выбирать intent:
- "note" — пользователь записывает факт, мысль, событие, которое уже произошло. Нет действия в будущем, нет времени. title оставь пустым "".
- "task" — пользователь хочет добавить задачу-чеклист (есть действие, но НЕТ конкретного времени). title — короткое название задачи без слов "сделать", "нужно".
- "reminder" — пользователь хочет напоминание (есть указание времени или периодичности: "в 9", "каждый день", "завтра", "по пятницам"). title оставь пустым "".
- "unknown" — непонятно, вопрос или команда. title пустой.

Примеры:
"Купил молоко" → {"intent":"note","confidence":0.9,"title":""}
"Сегодня хороший день, выспался" → {"intent":"note","confidence":0.95,"title":""}
"Позвонить маме" → {"intent":"task","confidence":0.85,"title":"Позвонить маме"}
"Купить хлеб и кефир" → {"intent":"task","confidence":0.85,"title":"Купить хлеб и кефир"}
"Завтра в 9 утра позвонить маме" → {"intent":"reminder","confidence":0.95,"title":""}
"Каждый понедельник в 10 планёрка" → {"intent":"reminder","confidence":0.95,"title":""}
"Что у меня сегодня?" → {"intent":"unknown","confidence":0.9,"title":""}

Возвращай ТОЛЬКО JSON, без markdown, без пояснений. Все ключи и строки в двойных кавычках.`

// ClassifyIntent классифицирует произвольное сообщение пользователя.
// Для intent=reminder параметры напоминания берутся отдельным вызовом ParseReminder —
// одна короткая schema на запрос стабильнее, чем вложенные.
func (c *LLMClient) ClassifyIntent(ctx context.Context, text, currentDate string) (*LLMIntentResult, error) {
	content, err := c.chat(ctx, fmt.Sprintf(intentSystemPrompt, currentDate), text, intentSchema, 128)
	if err != nil {
		return nil, err
	}

	var result LLMIntentResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse LLM result: %w", err)
	}

	return &result, nil
}
