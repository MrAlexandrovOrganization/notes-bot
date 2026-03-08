package bot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
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
// It retries on error with a fixed 5-second delay.
func RunKafkaConsumer(ctx context.Context, bootstrapServers string, handler func(context.Context, ReminderEvent), logger *zap.Logger) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		attempt++
		logger.Info("kafka consumer: creating reader",
			zap.String("brokers", bootstrapServers),
			zap.String("topic", "reminders_due"),
			zap.Int("attempt", attempt),
		)
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     []string{bootstrapServers},
			Topic:       "reminders_due",
			Partition:   0,
			StartOffset: kafka.LastOffset,
		})

		logger.Info("kafka consumer started, waiting for messages")
		if err := consume(ctx, r, handler, logger); err != nil {
			logger.Warn("kafka consumer error, retrying in 5s",
				zap.Error(err),
				zap.Int("attempt", attempt),
			)
			r.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		r.Close()
		return
	}
}

func consume(ctx context.Context, r *kafka.Reader, handler func(context.Context, ReminderEvent), logger *zap.Logger) error {
	defer r.Close()
	for {
		logger.Debug("kafka consumer: waiting for next message")
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			logger.Error("kafka FetchMessage error", zap.Error(err))
			return err
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
			continue
		}

		logger.Info("kafka consumer: dispatching reminder event",
			zap.Int64("user_id", ev.UserID),
			zap.Int64("reminder_id", ev.ReminderID),
			zap.String("title", ev.Title),
		)
		handler(ctx, ev)
	}
}
