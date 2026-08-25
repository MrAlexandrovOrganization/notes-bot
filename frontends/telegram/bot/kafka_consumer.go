package bot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"notes-bot/internal/applog"
	"notes-bot/internal/kafkacarrier"
)

const (
	kafkaTopic   = "reminders_due"
	kafkaGroupID = "telegram-bot-reminders"

	// maxHandlerAttempts is the number of delivery attempts per message.
	// After the last failed attempt the message is committed and skipped
	// (poison-message policy) so a permanently failing notification cannot
	// block the queue. Delivery semantics are at-least-once: transient
	// failures are retried, exhausted ones are dropped with an error metric.
	maxHandlerAttempts = 3

	handlerRetryBaseDelay = 5 * time.Second
)

// ReminderEvent is the payload published to the reminders_due Kafka topic.
type ReminderEvent struct {
	UserID     int64  `json:"user_id"`
	Title      string `json:"title"`
	ReminderID int64  `json:"reminder_id"`
	CreateTask bool   `json:"create_task"`
	TodayDate  string `json:"today_date"`
	IsActive   bool   `json:"is_active"`
}

// RunKafkaConsumer reads from the reminders_due Kafka topic and invokes handler for each event.
// Uses a consumer group so Kafka tracks committed offsets — no external offset store needed.
// On first join (no committed offset) starts from the tail to avoid replaying history.
// On error retries with a 5-second delay; Kafka resumes from the last committed offset.
func RunKafkaConsumer(ctx context.Context, bootstrapServers string, handler func(context.Context, ReminderEvent) error, logger *zap.Logger) {
	log := applog.With(ctx, logger)

	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return
		}

		log.Info("kafka consumer: creating reader",
			zap.String("brokers", bootstrapServers),
			zap.String("topic", kafkaTopic),
			zap.String("group_id", kafkaGroupID),
			zap.Int("attempt", attempt),
		)

		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:        []string{bootstrapServers},
			Topic:          kafkaTopic,
			GroupID:        kafkaGroupID,
			CommitInterval: time.Second,
			StartOffset:    kafka.LastOffset, // fallback when no committed offset exists yet
		})

		log.Info("kafka consumer started, waiting for messages")
		err := consume(ctx, r, handler, logger)
		r.Close()

		if err == nil {
			return
		}

		log.Warn("kafka consumer error, retrying in 5s",
			zap.Error(err),
			zap.Int("attempt", attempt),
		)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// consume reads messages until ctx is cancelled or an error occurs.
// Commits each message after successful processing so Kafka tracks progress.
func consume(ctx context.Context, r *kafka.Reader, handler func(context.Context, ReminderEvent) error, logger *zap.Logger) error {
	log := applog.With(ctx, logger)
	for {
		log.Debug("kafka consumer: waiting for next message")
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			log.Error("kafka FetchMessage error", zap.Error(err))
			return err
		}

		log.Info("kafka consumer: message received",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", int32(msg.Partition)),
			zap.Int64("offset", msg.Offset),
			zap.Int("value_len", len(msg.Value)),
		)

		var ev ReminderEvent
		if err := json.Unmarshal(msg.Value, &ev); err != nil {
			log.Error("failed to parse reminder event",
				zap.Error(err),
				zap.String("raw_value", string(msg.Value)),
			)
			commitMsg(ctx, r, msg, log)
			continue
		}

		log.Info("kafka consumer: dispatching reminder event",
			zap.Int64("user_id", ev.UserID),
			zap.Int64("reminder_id", ev.ReminderID),
			zap.String("title", ev.Title),
		)

		if !ev.IsActive {
			log.Info("reminder is not active, skipping")
			commitMsg(ctx, r, msg, log)
			continue
		}

		carrier := kafkacarrier.HeaderCarrier(msg.Headers)
		propagatedCtx := otel.GetTextMapPropagator().Extract(ctx, &carrier)
		msgCtx, msgSpan := otel.Tracer("telegram/kafka").Start(propagatedCtx, "kafka.consume "+kafkaTopic,
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "kafka"),
				attribute.Int64("messaging.kafka.offset", msg.Offset),
				attribute.Int64("reminder_id", ev.ReminderID),
			),
		)
		var lastErr error
		for attempt := 1; attempt <= maxHandlerAttempts; attempt++ {
			handlerErr := handler(msgCtx, ev)
			if handlerErr == nil {
				lastErr = nil
				break
			}
			lastErr = handlerErr
			msgSpan.RecordError(handlerErr)
			msgSpan.SetStatus(codes.Error, handlerErr.Error())
			log.Error("reminder handler failed",
				zap.Error(handlerErr),
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxHandlerAttempts),
				zap.Int64("offset", msg.Offset),
				zap.Int64("reminder_id", ev.ReminderID),
			)
			if attempt < maxHandlerAttempts {
				delay := handlerRetryBaseDelay * time.Duration(attempt)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
		}
		if lastErr != nil {
			// Poison message: all attempts failed. Commit and skip so the
			// queue keeps moving; the loss is visible via the error metric.
			ReminderDeliveryErrors.Add(msgCtx, 1)
			log.Error("reminder handler failed permanently, committing and skipping message",
				zap.Error(lastErr),
				zap.Int64("offset", msg.Offset),
				zap.Int64("reminder_id", ev.ReminderID),
			)
			msgSpan.End()
			commitMsg(ctx, r, msg, log)
			continue
		}
		commitMsg(ctx, r, msg, log)
		KafkaMessagesConsumed.Add(msgCtx, 1)
		msgSpan.End()
	}
}

func commitMsg(ctx context.Context, r *kafka.Reader, msg kafka.Message, log *zap.Logger) {
	if err := r.CommitMessages(ctx, msg); err != nil {
		log.Error("failed to commit kafka message",
			zap.Error(err),
			zap.Int64("offset", msg.Offset),
		)
	}
}
