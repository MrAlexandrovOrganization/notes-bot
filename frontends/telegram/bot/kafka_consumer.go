package bot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"notes_bot/internal/kafkacarrier"
)

// ReminderEvent is the payload published to the reminders_due Kafka topic.
type ReminderEvent struct {
	UserID     int64  `json:"user_id"`
	Title      string `json:"title"`
	ReminderID int64  `json:"reminder_id"`
	CreateTask bool   `json:"create_task"`
	TodayDate  string `json:"today_date"`
}

// RunKafkaConsumer reads from the reminders_due Kafka topic and invokes handler for each event.
// It retries on error with a fixed 5-second delay, resuming from the last seen offset.
func RunKafkaConsumer(ctx context.Context, bootstrapServers string, handler func(context.Context, ReminderEvent), logger *zap.Logger) {
	// nextOffset tracks where to resume after a transient error within the same session.
	// Starts at LastOffset so we don't replay historical messages on bot startup.
	nextOffset := kafka.LastOffset
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		attempt++
		logger.Info("kafka consumer: creating reader",
			zap.String("brokers", bootstrapServers),
			zap.String("topic", "reminders_due"),
			zap.Int64("start_offset", nextOffset),
			zap.Int("attempt", attempt),
		)
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     []string{bootstrapServers},
			Topic:       "reminders_due",
			Partition:   0,
			StartOffset: nextOffset,
		})

		logger.Info("kafka consumer started, waiting for messages")
		lastSeen, err := consume(ctx, r, handler, logger)
		r.Close()
		if lastSeen >= 0 {
			// Resume from the message after the last successfully processed one.
			nextOffset = lastSeen + 1
		}
		if err != nil {
			logger.Warn("kafka consumer error, retrying in 5s",
				zap.Error(err),
				zap.Int("attempt", attempt),
				zap.Int64("next_offset", nextOffset),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		return
	}
}

// consume reads messages until ctx is cancelled or an error occurs.
// Returns the offset of the last successfully processed message (-1 if none), and any error.
func consume(ctx context.Context, r *kafka.Reader, handler func(context.Context, ReminderEvent), logger *zap.Logger) (int64, error) {
	lastOffset := int64(-1)
	for {
		logger.Debug("kafka consumer: waiting for next message")
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			logger.Error("kafka FetchMessage error", zap.Error(err))
			return lastOffset, err
		}

		logger.Info("kafka consumer: message received",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", int32(msg.Partition)),
			zap.Int64("offset", msg.Offset),
			zap.Int("value_len", len(msg.Value)),
			zap.String("value", string(msg.Value)),
		)

		var ev ReminderEvent
		if err := json.Unmarshal(msg.Value, &ev); err != nil {
			logger.Error("failed to parse reminder event",
				zap.Error(err),
				zap.String("raw_value", string(msg.Value)),
			)
			lastOffset = msg.Offset
			continue
		}

		logger.Info("kafka consumer: dispatching reminder event",
			zap.Int64("user_id", ev.UserID),
			zap.Int64("reminder_id", ev.ReminderID),
			zap.String("title", ev.Title),
		)

		carrier := kafkacarrier.HeaderCarrier(msg.Headers)
		propagatedCtx := otel.GetTextMapPropagator().Extract(ctx, &carrier)
		msgCtx, span := otel.Tracer("telegram/kafka").Start(propagatedCtx, "kafka.consume reminders_due",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.Int64("messaging.kafka.offset", msg.Offset),
				attribute.Int64("reminder_id", ev.ReminderID),
			),
		)
		handler(msgCtx, ev)
		span.End()
		lastOffset = msg.Offset
	}
}
