package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"go.uber.org/zap"
)

const locationIndex = "location_history"

type ESClient struct {
	client *elasticsearch.Client
	logger *zap.Logger
}

func NewESClient(host string, port int) (*ESClient, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{
			fmt.Sprintf("http://%s:%d", host, port),
		},
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create es client: %w", err)
	}

	esc := &ESClient{client: client, logger: logger}

	if err := esc.ensureIndex(context.Background()); err != nil {
		esc.logger.Warn("es index creation failed (will retry on write)", zap.Error(err))
	}

	return esc, nil
}

func (c *ESClient) ensureIndex(ctx context.Context) error {
	res, err := c.client.Indices.Exists([]string{locationIndex})
	if err != nil {
		return fmt.Errorf("index exists check: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == 200 {
		return nil
	}

	mapping := map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"location": map[string]any{
					"type": "geo_point",
				},
				"timestamp": map[string]any{
					"type": "date",
				},
				"user_id": map[string]any{
					"type": "long",
				},
				"latitude": map[string]any{
					"type": "double",
				},
				"longitude": map[string]any{
					"type": "double",
				},
				"accuracy": map[string]any{
					"type": "float",
				},
				"source": map[string]any{
					"type": "keyword",
				},
			},
		},
		"settings": map[string]any{
			"number_of_shards":   1,
			"number_of_replicas": 0,
		},
	}

	body, _ := json.Marshal(mapping)
	res, err = c.client.Indices.Create(
		locationIndex,
		c.client.Indices.Create.WithBody(bytes.NewReader(body)),
		c.client.Indices.Create.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("create index error: %s", res.String())
	}

	c.logger.Info("es index created", zap.String("index", locationIndex))
	return nil
}

type LocationDoc struct {
	UserID    int64     `json:"user_id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Accuracy  *float64  `json:"accuracy,omitempty"`
	Altitude  *float64  `json:"altitude,omitempty"`
	Heading   *float64  `json:"heading,omitempty"`
	Speed     *float64  `json:"speed,omitempty"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
}

func (c *ESClient) IndexLocation(ctx context.Context, loc *LocationRecord) error {
	doc := LocationDoc{
		UserID:    loc.UserID,
		Latitude:  loc.Latitude,
		Longitude: loc.Longitude,
		Accuracy:  loc.Accuracy,
		Altitude:  loc.Altitude,
		Heading:   loc.Heading,
		Speed:     loc.Speed,
		Source:    loc.Source,
		Timestamp: loc.RecordedAt,
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal doc: %w", err)
	}

	res, err := c.client.Index(
		locationIndex,
		bytes.NewReader(body),
		c.client.Index.WithContext(ctx),
		c.client.Index.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("index doc: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() && !strings.Contains(res.String(), "resource_not_found_exception") {
		return fmt.Errorf("index error: %s", res.String())
	}

	return nil
}

func (c *ESClient) Ping(ctx context.Context) error {
	res, err := c.client.Ping(c.client.Ping.WithContext(ctx))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("es ping: %s", res.String())
	}
	return nil
}
