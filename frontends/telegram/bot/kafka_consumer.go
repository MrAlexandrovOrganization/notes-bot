package bot

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"notes_bot/internal/applog"
	"notes_bot/internal/kafkacarrier"
	"notes_bot/internal/telemetry"
)

const redisOffsetKey = "kafka:reminders_due:offset"

// OffsetStore persists the last consumed Kafka offset across restarts.
type OffsetStore interface {
	Load(ctx context.Context) int64
	Save(ctx context.Context, offset int64)
}

// RedisOffsetStore persists the Kafka consumer offset in Redis.
// On first run (key absent) it returns kafka.LastOffset so the consumer
// starts from the tail of the topic instead of replaying history.
type RedisOffsetStore struct {
	rdb    *redis.Client
	logger *zap.Logger
}

func NewRedisOffsetStore(rdb *redis.Client, logger *zap.Logger) *RedisOffsetStore {
	return &RedisOffsetStore{rdb: rdb, logger: logger}
}

// Load returns the offset to start reading from.
// If a committed offset exists, resumes from committed+1.
// Otherwise returns kafka.LastOffset (tail of topic).
func (s *RedisOffsetStore) Load(ctx context.Context) int64 {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	val, err := s.rdb.Get(ctx, redisOffsetKey).Int64()
	if err != nil {
		// Key not found or Redis error — start from the tail to avoid
		// replaying all historical messages on first boot.
		return kafka.LastOffset
	}
	return val + 1
}

// Save commits the offset of the last successfully processed message.
func (s *RedisOffsetStore) Save(ctx context.Context, offset int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, s.logger)
	if err := s.rdb.Set(ctx, redisOffsetKey, strconv.FormatInt(offset, 10), 0).Err(); err != nil {
		log.Error("failed to persist kafka offset", zap.Error(err), zap.Int64("offset", offset))
	}
}

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
// It retries on error with a fixed 5-second delay, resuming from the last committed offset.
// The committed offset is persisted via store so restarts do not replay old messages.
func RunKafkaConsumer(ctx context.Context, bootstrapServers string, store OffsetStore, handler func(context.Context, ReminderEvent), logger *zap.Logger) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, logger)
	nextOffset := store.Load(ctx)
	log.Info("kafka consumer: loaded start offset", zap.Int64("start_offset", nextOffset))

	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		attempt++
		log.Info("kafka consumer: creating reader",
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

		log.Info("kafka consumer started, waiting for messages")
		lastSeen, err := consume(ctx, r, store, handler, logger)
		r.Close()
		if lastSeen >= 0 {
			// In-memory resume position for the next retry within the same session.
			nextOffset = lastSeen + 1
		}
		if err != nil {
			log.Warn("kafka consumer error, retrying in 5s",
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
func consume(ctx context.Context, r *kafka.Reader, store OffsetStore, handler func(context.Context, ReminderEvent), logger *zap.Logger) (int64, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, logger)
	lastOffset := int64(-1)
	for {
		log.Debug("kafka consumer: waiting for next message")
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			log.Error("kafka FetchMessage error", zap.Error(err))
			return lastOffset, err
		}

		log.Info("kafka consumer: message received",
			zap.String("topic", msg.Topic),
			zap.Int32("partition", int32(msg.Partition)),
			zap.Int64("offset", msg.Offset),
			zap.Int("value_len", len(msg.Value)),
			zap.String("value", string(msg.Value)),
		)

		var ev ReminderEvent
		if err := json.Unmarshal(msg.Value, &ev); err != nil {
			log.Error("failed to parse reminder event",
				zap.Error(err),
				zap.String("raw_value", string(msg.Value)),
			)
			lastOffset = msg.Offset
			store.Save(ctx, msg.Offset)
			continue
		}

		log.Info("kafka consumer: dispatching reminder event",
			zap.Int64("user_id", ev.UserID),
			zap.Int64("reminder_id", ev.ReminderID),
			zap.String("title", ev.Title),
		)
		if !ev.IsActive {
			log.Info("remainder is not active")
			continue
		}

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
		store.Save(ctx, msg.Offset)
	}
}
