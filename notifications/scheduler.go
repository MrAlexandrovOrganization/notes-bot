package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"notes_bot/internal/kafkacarrier"
	"notes_bot/internal/telemetry"
	"notes_bot/internal/timeutil"
	pb "notes_bot/proto/notes"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// ComputeNextFire computes the next fire time after afterUTC for the given schedule.
// Returns nil for "once" (deactivate after firing).
func ComputeNextFire(scheduleType string, params map[string]any, afterUTC time.Time, tzOffsetHours int) *time.Time {
	tzOffset := time.Duration(tzOffsetHours) * time.Hour
	afterLocal := afterUTC.In(time.FixedZone("local", int(tzOffset.Seconds())))

	tz := afterLocal.Location()

	hour := paramInt(params, "hour", 9)
	minute := paramInt(params, "minute", 0)

	switch scheduleType {
	case "once":
		return nil

	case "daily":
		candidate := time.Date(afterLocal.Year(), afterLocal.Month(), afterLocal.Day(), hour, minute, 0, 0, tz)
		if !candidate.After(afterLocal) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		utc := candidate.UTC()
		return &utc

	case "weekly":
		days := paramIntSlice(params, "days", []int{0})
		candidate := time.Date(afterLocal.Year(), afterLocal.Month(), afterLocal.Day(), hour, minute, 0, 0, tz)
		if !candidate.After(afterLocal) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		for i := 0; i < 7; i++ {
			wd := int(candidate.Weekday()+6) % 7 // Monday=0
			for _, d := range days {
				if d == wd {
					utc := candidate.UTC()
					return &utc
				}
			}
			candidate = candidate.AddDate(0, 0, 1)
		}
		return nil

	case "monthly":
		dayOfMonth := paramInt(params, "day_of_month", 1)
		candidate := safeDate(afterLocal.Year(), afterLocal.Month(), dayOfMonth, hour, minute, tz)
		if candidate == nil || !candidate.After(afterLocal) {
			// advance to next month
			year, month := afterLocal.Year(), afterLocal.Month()+1
			if month > 12 {
				month = 1
				year++
			}
			candidate = safeDate(year, month, dayOfMonth, hour, minute, tz)
		}
		if candidate == nil {
			return nil
		}
		utc := candidate.UTC()
		return &utc

	case "yearly":
		month := time.Month(paramInt(params, "month", 1))
		day := paramInt(params, "day", 1)
		candidate := safeDate(afterLocal.Year(), month, day, hour, minute, tz)
		if candidate == nil || !candidate.After(afterLocal) {
			candidate = safeDate(afterLocal.Year()+1, month, day, hour, minute, tz)
		}
		if candidate == nil {
			return nil
		}
		utc := candidate.UTC()
		return &utc

	case "custom_days":
		intervalDays := paramInt(params, "interval_days", 1)
		candidate := time.Date(afterLocal.Year(), afterLocal.Month(), afterLocal.Day(), hour, minute, 0, 0, tz)
		if !candidate.After(afterLocal) {
			candidate = candidate.AddDate(0, 0, intervalDays)
		}
		utc := candidate.UTC()
		return &utc
	}

	return nil
}

func safeDate(year int, month time.Month, day, hour, minute int, loc *time.Location) *time.Time {
	// Validate day in month
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	if day > daysInMonth {
		return nil
	}
	t := time.Date(year, month, day, hour, minute, 0, 0, loc)
	return &t
}

func paramInt(params map[string]any, key string, def int) int {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n)
		}
	}
	return def
}

func paramIntSlice(params map[string]any, key string, def []int) []int {
	v, ok := params[key]
	if !ok {
		return def
	}
	arr, ok := v.([]any)
	if !ok {
		return def
	}
	result := make([]int, 0, len(arr))
	for _, item := range arr {
		switch x := item.(type) {
		case float64:
			result = append(result, int(x))
		case int:
			result = append(result, x)
		}
	}
	return result
}

type Scheduler struct {
	pool     *pgxpool.Pool
	producer *kafka.Writer
	cfg      *Config

	mu       sync.Mutex
	coreConn *grpc.ClientConn
	coreStub pb.NotesServiceClient
}

func NewScheduler(pool *pgxpool.Pool, cfg *Config) *Scheduler {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(cfg.KafkaBootstrapServers),
		Topic:                  "reminders_due",
		RequiredAcks:           kafka.RequireOne,
		AllowAutoTopicCreation: true,
	}
	return &Scheduler{
		pool:     pool,
		producer: w,
		cfg:      cfg,
	}
}

func (s *Scheduler) getCoreStub() pb.NotesServiceClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.coreStub == nil {
		addr := fmt.Sprintf("%s:%s", s.cfg.CoreGRPCHost, s.cfg.CoreGRPCPort)
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		)
		if err != nil {
			logger.Error("failed to dial core", zap.Error(err))
			return nil
		}
		s.coreConn = conn
		s.coreStub = pb.NewNotesServiceClient(conn)
	}
	return s.coreStub
}

func (s *Scheduler) getTodayDateStr(ctx context.Context) string {
	stub := s.getCoreStub()
	if stub == nil {
		return s.localTodayDate()
	}
	resp, err := stub.GetTodayDate(ctx, &pb.Empty{})
	if err != nil {
		logger.Error("failed to get today date from core", zap.Error(err))
		return s.localTodayDate()
	}
	return resp.Date
}

func (s *Scheduler) localTodayDate() string {
	return timeutil.TodayDate(s.cfg.TimezoneOffsetHours, 0)
}

func (s *Scheduler) addTaskToToday(ctx context.Context, title, todayDate string) {
	stub := s.getCoreStub()
	if stub == nil {
		return
	}
	if _, err := stub.EnsureNote(ctx, &pb.DateRequest{Date: todayDate}); err != nil {
		logger.Error("failed to ensure note", zap.Error(err))
		return
	}
	if _, err := stub.AddTask(ctx, &pb.AddTaskRequest{Date: todayDate, TaskText: title}); err != nil {
		logger.Error("failed to add task", zap.Error(err))
	}
}

type reminderEvent struct {
	UserID     int64  `json:"user_id"`
	Title      string `json:"title"`
	ReminderID int64  `json:"reminder_id"`
	CreateTask bool   `json:"create_task"`
	TodayDate  string `json:"today_date"`
}

func (s *Scheduler) publishEvent(ctx context.Context, ev reminderEvent) {
	ctx, span := otel.Tracer("notifications/scheduler").Start(ctx, "kafka.produce reminders_due",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", "reminders_due"),
			attribute.Int64("reminder_id", ev.ReminderID),
		),
	)
	defer span.End()

	data, err := json.Marshal(ev)
	if err != nil {
		logger.Error("marshal event", zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	headers := make(kafkacarrier.HeaderCarrier, 0)
	otel.GetTextMapPropagator().Inject(ctx, &headers)

	logger.Debug("publishing reminder event to kafka",
		zap.Int64("reminder_id", ev.ReminderID),
		zap.Int64("user_id", ev.UserID),
		zap.String("title", ev.Title),
		zap.String("payload", string(data)),
	)
	if err := s.producer.WriteMessages(ctx, kafka.Message{
		Value:   data,
		Headers: []kafka.Header(headers),
	}); err != nil {
		logger.Error("write kafka message failed",
			zap.Int64("reminder_id", ev.ReminderID),
			zap.Error(err),
		)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}
	logger.Info("reminder event published to kafka",
		zap.Int64("reminder_id", ev.ReminderID),
		zap.Int64("user_id", ev.UserID),
	)
}

func (s *Scheduler) tick(ctx context.Context) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	due, err := GetDueReminders(ctx, s.pool)
	if err != nil {
		logger.Error("get due reminders", zap.Error(err))
		return
	}

	for _, r := range due {
		todayDate := ""
		if r.CreateTask {
			todayDate = s.getTodayDateStr(ctx)
			s.addTaskToToday(ctx, r.Title, todayDate)
		}

		s.publishEvent(ctx, reminderEvent{
			UserID:     r.UserID,
			Title:      r.Title,
			ReminderID: r.ID,
			CreateTask: r.CreateTask,
			TodayDate:  todayDate,
		})

		nextFire := ComputeNextFire(r.ScheduleType, r.ScheduleParams, time.Now().UTC(), s.cfg.TimezoneOffsetHours)
		if err := UpdateNextFire(ctx, s.pool, r.ID, nextFire); err != nil {
			logger.Error("update next fire", zap.Int64("id", r.ID), zap.Error(err))
		}

		logger.Info("fired reminder",
			zap.Int64("id", r.ID),
			zap.Int64("user_id", r.UserID),
		)
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	interval := time.Duration(s.cfg.SchedulerIntervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Info("scheduler started", zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			logger.Info("scheduler stopped")
			s.mu.Lock()
			if s.coreConn != nil {
				s.coreConn.Close()
			}
			s.mu.Unlock()
			s.producer.Close()
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}
