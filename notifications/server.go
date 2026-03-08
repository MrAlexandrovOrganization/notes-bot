package notifications

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes_bot/proto/notifications"
)

type NotificationsServer struct {
	pb.UnimplementedNotificationsServiceServer
	pool *pgxpool.Pool
	cfg  *Config
}

func NewNotificationsServer(pool *pgxpool.Pool, cfg *Config) *NotificationsServer {
	return &NotificationsServer{pool: pool, cfg: cfg}
}

func reminderToProto(r *Reminder) *pb.Reminder {
	paramsJSON, _ := json.Marshal(r.ScheduleParams)
	return &pb.Reminder{
		Id:                 r.ID,
		UserId:             r.UserID,
		Title:              r.Title,
		ScheduleType:       r.ScheduleType,
		ScheduleParamsJson: string(paramsJSON),
		NextFireAt:         r.NextFireAt.UTC().Format(time.RFC3339),
		IsActive:           r.IsActive,
		CreateTask:         r.CreateTask,
	}
}

func (s *NotificationsServer) CreateReminder(ctx context.Context, req *pb.CreateReminderRequest) (*pb.ReminderResponse, error) {
	var params map[string]any
	if req.ScheduleParamsJson != "" {
		if err := json.Unmarshal([]byte(req.ScheduleParamsJson), &params); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid schedule_params_json")
		}
	} else {
		params = map[string]any{}
	}

	// Compute initial next fire
	tzOffset := getParamInt(params, "tz_offset", s.cfg.TimezoneOffsetHours)
	nowUTC := time.Now().UTC()

	var nextFireAt time.Time
	if req.ScheduleType == "once" {
		dateStr := getParamStr(params, "date", "")
		hour := getParamInt(params, "hour", 9)
		minute := getParamInt(params, "minute", 0)
		loc := time.FixedZone("tz", tzOffset*3600)
		d, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			nextFireAt = nowUTC
		} else {
			nextFireAt = time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc).UTC()
		}
	} else {
		next := ComputeNextFire(req.ScheduleType, params, nowUTC, tzOffset)
		if next != nil {
			nextFireAt = *next
		} else {
			nextFireAt = nowUTC
		}
	}

	// Reject if in the past
	if !nextFireAt.After(nowUTC) {
		return nil, status.Error(codes.InvalidArgument, "Reminder date is in the past")
	}

	r, err := CreateReminder(ctx, s.pool, req.UserId, req.Title, req.ScheduleType, params, nextFireAt, req.CreateTask)
	if err != nil {
		logger.Error("create reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.ReminderResponse{Success: true, Reminder: reminderToProto(r)}, nil
}

func (s *NotificationsServer) ListReminders(ctx context.Context, req *pb.ListRemindersRequest) (*pb.ListRemindersResponse, error) {
	rows, err := ListReminders(ctx, s.pool, req.UserId)
	if err != nil {
		logger.Error("list reminders", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	reminders := make([]*pb.Reminder, len(rows))
	for i, r := range rows {
		reminders[i] = reminderToProto(r)
	}
	return &pb.ListRemindersResponse{Reminders: reminders}, nil
}

func (s *NotificationsServer) DeleteReminder(ctx context.Context, req *pb.DeleteReminderRequest) (*pb.SuccessResponse, error) {
	ok, err := DeleteReminder(ctx, s.pool, req.ReminderId, req.UserId)
	if err != nil {
		logger.Error("delete reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SuccessResponse{Success: ok}, nil
}

func (s *NotificationsServer) PostponeReminder(ctx context.Context, req *pb.PostponeReminderRequest) (*pb.ReminderResponse, error) {
	var nextFireAt time.Time

	switch {
	case req.TargetDate != "":
		loc := time.FixedZone("tz", s.cfg.TimezoneOffsetHours*3600)
		d, err := time.ParseInLocation("2006-01-02", req.TargetDate, loc)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid target_date")
		}
		nextFireAt = time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc).UTC()

	case req.PostponeHours > 0:
		nextFireAt = time.Now().UTC().Add(time.Duration(req.PostponeHours) * time.Hour)

	default:
		days := int(req.PostponeDays)
		if days <= 0 {
			days = 1
		}
		nextFireAt = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	}

	ok, err := SetNextFireAt(ctx, s.pool, req.ReminderId, req.UserId, nextFireAt)
	if err != nil {
		logger.Error("postpone reminder", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &pb.ReminderResponse{
		Success: ok,
		Reminder: &pb.Reminder{
			Id:         req.ReminderId,
			UserId:     req.UserId,
			NextFireAt: nextFireAt.UTC().Format(time.RFC3339),
		},
	}, nil
}

func getParamInt(params map[string]any, key string, def int) int {
	return paramInt(params, key, def)
}

func getParamStr(params map[string]any, key string, def string) string {
	v, ok := params[key]
	if !ok {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}
