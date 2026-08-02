package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var ErrChatUnavailable = errors.New("chat model unavailable")

// ChatClient is the small Ollama client shared by profile extraction and the
// read-only search agent. Keeping it in the search service means Telegram and
// web use exactly the same retrieval policy.
type ChatClient struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewChatClient(host, port, model string) *ChatClient {
	return &ChatClient{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		model:   model,
		http: &http.Client{
			Timeout:   10 * time.Minute,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Format   any            `json:"format,omitempty"`
	Think    *bool          `json:"think,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

var reasoningBlock = regexp.MustCompile(`(?is)<think>.*?</think>`)

func (c *ChatClient) call(ctx context.Context, system, user string, schema any, numPredict int, temperature float64) (string, error) {
	if numPredict <= 0 {
		numPredict = 512
	}
	think := false
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
		Format: schema,
		Think:  &think,
		Options: map[string]any{
			"num_predict": numPredict,
			"temperature": temperature,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrChatUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("%w: status %d: %s", ErrChatUnavailable, resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	return strings.TrimSpace(reasoningBlock.ReplaceAllString(decoded.Message.Content, "")), nil
}

func (c *ChatClient) JSON(ctx context.Context, system, user string, schema any, dst any, numPredict int) error {
	content, err := c.call(ctx, system, user, schema, numPredict, 0)
	if err != nil {
		return err
	}
	content = extractJSONObject(content)
	if err := json.Unmarshal([]byte(content), dst); err != nil {
		return fmt.Errorf("decode structured chat result: %w", err)
	}
	return nil
}

func (c *ChatClient) Text(ctx context.Context, system, user string, numPredict int) (string, error) {
	return c.call(ctx, system, user, nil, numPredict, 0.2)
}

func extractJSONObject(value string) string {
	start := strings.IndexByte(value, '{')
	end := strings.LastIndexByte(value, '}')
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}
