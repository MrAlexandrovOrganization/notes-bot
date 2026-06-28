package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notes-bot/internal/telemetry"
)

// ErrEmbedderUnavailable is returned when Ollama is unreachable or returns a
// non-2xx status. Callers can compare with errors.Is to fall back gracefully.
var ErrEmbedderUnavailable = errors.New("embedder unavailable")

// Embedder issues embedding requests against Ollama /api/embed.
type Embedder struct {
	baseURL string
	model   string
	dim     int
	http    *http.Client
}

// NewEmbedder builds an embedder targeting Ollama at host:port with the given model.
// The dim parameter is the expected vector dimension; returned vectors that do
// not match are rejected as an error.
func NewEmbedder(host, port, model string, dim int) *Embedder {
	return &Embedder{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		model:   model,
		dim:     dim,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed sends the inputs as a batch to /api/embed and returns one vector per
// input in the same order. The metrics counter (if non-nil) is incremented per
// batch call (not per input) so it matches Ollama's billing model.
func (e *Embedder) Embed(ctx context.Context, inputs []string, metrics *Metrics) ([][]float32, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	if len(inputs) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(ollamaEmbedRequest{Model: e.model, Input: inputs})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if metrics != nil {
		metrics.embedCalls.Add(ctx, 1)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrEmbedderUnavailable, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return nil, fmt.Errorf("%w: status %d", ErrEmbedderUnavailable, resp.StatusCode)
		}
		return nil, fmt.Errorf("%w: status %d: %s", ErrEmbedderUnavailable, resp.StatusCode, msg)
	}

	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(out.Embeddings), len(inputs))
	}
	for i, v := range out.Embeddings {
		if len(v) != e.dim {
			return nil, fmt.Errorf("embedder returned dim=%d, expected %d (input %d)", len(v), e.dim, i)
		}
	}
	return out.Embeddings, nil
}

// EmbedOne is a convenience wrapper around Embed for single-input callers (e.g. queries).
func (e *Embedder) EmbedOne(ctx context.Context, input string, metrics *Metrics) ([]float32, error) {
	vs, err := e.Embed(ctx, []string{input}, metrics)
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}
