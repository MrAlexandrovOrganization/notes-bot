package kafkacarrier

import "github.com/segmentio/kafka-go"

// HeaderCarrier adapts []kafka.Header to the otel TextMapCarrier interface,
// allowing trace context to be injected into and extracted from Kafka message headers.
type HeaderCarrier []kafka.Header

func (c *HeaderCarrier) Get(key string) string {
	for _, h := range *c {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c *HeaderCarrier) Set(key, value string) {
	// Remove existing header with same key, then append.
	updated := (*c)[:0]
	for _, h := range *c {
		if h.Key != key {
			updated = append(updated, h)
		}
	}
	*c = append(updated, kafka.Header{Key: key, Value: []byte(value)})
}

func (c *HeaderCarrier) Keys() []string {
	keys := make([]string, len(*c))
	for i, h := range *c {
		keys[i] = h.Key
	}
	return keys
}
