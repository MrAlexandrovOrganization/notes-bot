package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/internal/applog"
	pb "notes-bot/proto/notifications"
)

type NotificationsServer struct {
	pb.UnimplementedNotificationsServiceServer
	pool    *pgxpool.Pool
	cfg     *Config
	metrics *notifMetrics
}

func NewNotificationsServer(pool *pgxpool.Pool, cfg *Config) *NotificationsServer {
	return &NotificationsServer{pool: pool, cfg: cfg, metrics: newNotifMetrics()}
}

func (s *NotificationsServer) recordRPC(ctx context.Context, method string, err *error) {
	st := "ok"
	if *err != nil {
		st = "error"
	}
	s.metrics.rpcRequests.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("method", method),
			attribute.String("status", st),
		),
	)
}

func reminderToProto(r *Reminder) *pb.Reminder {
	if r == nil {
		return &pb.Reminder{}
	}
	return &pb.Reminder{
		Id:           r.ID,
		UserId:       r.UserID,
		Title:        r.Title,
		ScheduleType: r.ScheduleType,
		NextFireAt:   timestamppb.New(r.NextFireAt.UTC()),
		IsActive:     r.IsActive,
		CreateTask:   r.CreateTask,
	}
}

// scheduleParamsToMap converts the typed proto ScheduleParams into the
// map[string]any used internally by ComputeNextFire and stored as JSONB.
func scheduleParamsToMap(sp *pb.ScheduleParams) map[string]any {
	if sp == nil {
		return map[string]any{}
	}
	params := map[string]any{
		"hour":      int(sp.Hour),
		"minute":    int(sp.Minute),
		"tz_offset": int(sp.TzOffset),
	}
	switch e := sp.Extra.(type) {
	case *pb.ScheduleParams_Weekly:
		if e.Weekly != nil {
			days := make([]int, len(e.Weekly.Days))
			for i, d := range e.Weekly.Days {
				days[i] = int(d)
			}
			params["days"] = days
		}
	case *pb.ScheduleParams_Monthly:
		if e.Monthly != nil {
			params["day_of_month"] = int(e.Monthly.DayOfMonth)
		}
	case *pb.ScheduleParams_Yearly:
		if e.Yearly != nil {
			params["month"] = int(e.Yearly.Month)
			params["day"] = int(e.Yearly.Day)
		}
	case *pb.ScheduleParams_Once:
		if e.Once != nil {
			params["date"] = e.Once.Date
		}
	case *pb.ScheduleParams_CustomDays:
		if e.CustomDays != nil {
			params["interval_days"] = int(e.CustomDays.IntervalDays)
		}
	}
	return params
}

// validateReminderRequest rejects malformed schedule parameters with a
// descriptive InvalidArgument instead of silently normalizing them into
// wrong fire times downstream.
func validateReminderRequest(scheduleType string, sp *pb.ScheduleParams) error {
	switch scheduleType {
	case "once", "daily", "weekly", "monthly", "yearly", "custom_days":
	default:
		return status.Errorf(codes.InvalidArgument, "unknown schedule_type %q", scheduleType)
	}
	if sp == nil {
		return status.Error(codes.InvalidArgument, "schedule_params is required")
	}
	if sp.Hour < 0 || sp.Hour > 23 {
		return status.Errorf(codes.InvalidArgument, "hour must be between 0 and 23, got %d", sp.Hour)
	}
	if sp.Minute < 0 || sp.Minute > 59 {
		return status.Errorf(codes.InvalidArgument, "minute must be between 0 and 59, got %d", sp.Minute)
	}
	switch e := sp.Extra.(type) {
	case nil:
		// daily has no extra params; anything else is a mismatch.
		if scheduleType != "daily" {
			return status.Errorf(codes.InvalidArgument, "schedule_params do not match schedule_type %q", scheduleType)
		}
	case *pb.ScheduleParams_Weekly:
		if e.Weekly == nil || len(e.Weekly.Days) == 0 {
			return status.Error(codes.InvalidArgument, "weekly schedule requires at least one day of week")
		}
		for _, d := range e.Weekly.Days {
			if d < 0 || d > 6 {
				return status.Errorf(codes.InvalidArgument, "weekday must be between 0 and 6, got %d", d)
			}
		}
	case *pb.ScheduleParams_Monthly:
		if e.Monthly == nil || e.Monthly.DayOfMonth < 1 || e.Monthly.DayOfMonth > 31 {
			return status.Error(codes.InvalidArgument, "day_of_month must be between 1 and 31")
		}
	case *pb.ScheduleParams_Yearly:
		if e.Yearly == nil {
			return status.Error(codes.InvalidArgument, "yearly params are required")
		}
		if e.Yearly.Month < 1 || e.Yearly.Month > 12 {
			return status.Errorf(codes.InvalidArgument, "month must be between 1 and 12, got %d", e.Yearly.Month)
		}
		if e.Yearly.Day < 1 || e.Yearly.Day > 31 {
			return status.Errorf(codes.InvalidArgument, "day must be between 1 and 31, got %d", e.Yearly.Day)
		}
	case *pb.ScheduleParams_Once:
		if e.Once == nil || e.Once.Date == "" {
			return status.Error(codes.InvalidArgument, "once schedule requires a date")
		}
	case *pb.ScheduleParams_CustomDays:
		if e.CustomDays == nil || e.CustomDays.IntervalDays < 1 {
			return status.Error(codes.InvalidArgument, "interval_days must be at least 1")
		}
	default:
		return status.Errorf(codes.InvalidArgument, "schedule_params do not match schedule_type %q", scheduleType)
	}
	return nil
}

func (s *NotificationsServer) CreateReminder(ctx context.Context, req *pb.CreateReminderRequest) (resp *pb.ReminderResponse, err error) {
	defer s.recordRPC(ctx, "CreateReminder", &err)
	log := applog.With(ctx, logger)

	if err := validateReminderRequest(req.ScheduleType, req.ScheduleParams); err != nil {
		return nil, err
	}

	params := scheduleParamsToMap(req.ScheduleParams)

	// Compute initial next fire
	tzOffset := paramInt(params, "tz_offset", s.cfg.TimezoneOffsetHours)
	nowUTC := time.Now().UTC()

	var nextFireAt time.Time
	if req.ScheduleType == "once" {
		dateStr := req.GetScheduleParams().GetOnce().GetDate()
		hour := paramInt(params, "hour", 9)
		minute := paramInt(params, "minute", 0)
		loc := time.FixedZone("tz", tzOffset*3600)
		d, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid date %q, expected YYYY-MM-DD", dateStr)
		}
		nextFireAt = time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc).UTC()
	} else {
		next := ComputeNextFire(ctx, req.ScheduleType, params, nowUTC, tzOffset)
		if next == nil {
			return nil, status.Errorf(codes.InvalidArgument, "cannot compute first fire time for schedule_type %q", req.ScheduleType)
		}
		nextFireAt = *next
	}

	// Reject if in the past
	if !nextFireAt.After(nowUTC) {
		return nil, status.Error(codes.InvalidArgument, "Reminder date is in the past")
	}

	r, err := CreateReminder(ctx, s.pool, req.UserId, req.Title, req.ScheduleType, params, nextFireAt, req.CreateTask)
	if err != nil {
		log.Error("create reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.ReminderResponse{Success: true, Reminder: reminderToProto(r)}, nil
}

func (s *NotificationsServer) ListReminders(ctx context.Context, req *pb.ListRemindersRequest) (resp *pb.ListRemindersResponse, err error) {
	defer s.recordRPC(ctx, "ListReminders", &err)
	log := applog.With(ctx, logger)
	rows, err := ListReminders(ctx, s.pool, req.UserId)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	reminders := make([]*pb.Reminder, len(rows))
	for i, r := range rows {
		reminders[i] = reminderToProto(r)
	}
	return &pb.ListRemindersResponse{Reminders: reminders}, nil
}

func (s *NotificationsServer) GetReminder(ctx context.Context, req *pb.GetReminderRequest) (resp *pb.ReminderResponse, err error) {
	defer s.recordRPC(ctx, "GetReminder", &err)
	log := applog.With(ctx, logger)
	r, err := GetReminder(ctx, s.pool, req.ReminderId, req.UserId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "reminder not found")
		}
		log.Error("get reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ReminderResponse{Success: true, Reminder: reminderToProto(r)}, nil
}

func (s *NotificationsServer) DeleteReminder(ctx context.Context, req *pb.DeleteReminderRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "DeleteReminder", &err)
	log := applog.With(ctx, logger)
	ok, err := DeleteReminder(ctx, s.pool, req.ReminderId, req.UserId)
	if err != nil {
		log.Error("delete reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SuccessResponse{Success: ok}, nil
}

func (s *NotificationsServer) PostponeReminder(ctx context.Context, req *pb.PostponeReminderRequest) (resp *pb.ReminderResponse, err error) {
	defer s.recordRPC(ctx, "PostponeReminder", &err)
	log := applog.With(ctx, logger)
	minutes := int(req.PostponeMinutes)
	if minutes <= 0 {
		minutes = 60
	}
	const maxPostponeMinutes = 10 * 365 * 24 * 60 // 10 years
	if minutes > maxPostponeMinutes {
		return nil, status.Errorf(codes.InvalidArgument, "postpone_minutes must not exceed %d", maxPostponeMinutes)
	}
	nextFireAt := time.Now().UTC().Add(time.Duration(minutes) * time.Minute)

	ok, err := SetNextFireAt(ctx, s.pool, req.ReminderId, req.UserId, nextFireAt)
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.ReminderResponse{
		Success: ok,
		Reminder: &pb.Reminder{
			Id:         req.ReminderId,
			UserId:     req.UserId,
			NextFireAt: timestamppb.New(nextFireAt.UTC()),
		},
	}, nil
}
