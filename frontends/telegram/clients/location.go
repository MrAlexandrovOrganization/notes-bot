package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// LocationService is the interface for the location HTTP service.
type LocationService interface {
	Save(ctx context.Context, input *SaveLocationInput) (*LocationRecord, error)
	ListByDate(ctx context.Context, date string) ([]*LocationRecord, error)
}

// Known location point sources. Add a new constant here when a new client
// starts saving locations.
const (
	SourceTelegramBot = "telegram-bot"
)

type SaveLocationInput struct {
	Latitude   float64
	Longitude  float64
	Accuracy   float32
	LivePeriod int
	Date       string
	RecordedAt time.Time
	Source     string
}

type LocationRecord struct {
	ID         string    `json:"id"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	Accuracy   float32   `json:"accuracy"`
	LivePeriod int       `json:"live_period"`
	Date       string    `json:"date"`
	Source     string    `json:"source"`
	RecordedAt time.Time `json:"recorded_at"`
}

type LocationClient struct {
	baseURL string
	http    *http.Client
}

func NewLocationClient(host, port string) *LocationClient {
	return &LocationClient{
		baseURL: fmt.Sprintf("http://%s:%s", host, port),
		http: &http.Client{
			Timeout:   10 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

func (c *LocationClient) Save(ctx context.Context, input *SaveLocationInput) (*LocationRecord, error) {
	body, err := json.Marshal(map[string]any{
		"latitude":    input.Latitude,
		"longitude":   input.Longitude,
		"accuracy":    input.Accuracy,
		"live_period": input.LivePeriod,
		"date":        input.Date,
		"source":      input.Source,
		"recorded_at": input.RecordedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/locations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errBody map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("location service: HTTP %d: %s", resp.StatusCode, errBody["error"])
	}

	var loc LocationRecord
	if err := json.NewDecoder(resp.Body).Decode(&loc); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &loc, nil
}

func (c *LocationClient) ListByDate(ctx context.Context, date string) ([]*LocationRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/locations?date="+date, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("location service: HTTP %d: %s", resp.StatusCode, errBody["error"])
	}

	var locs []*LocationRecord
	if err := json.NewDecoder(resp.Body).Decode(&locs); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return locs, nil
}

var _ LocationService = (*LocationClient)(nil)
