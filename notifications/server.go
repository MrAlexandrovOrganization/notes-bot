package notifications

import (
	"context"
	"time"

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
	es      *ESClient
}

func NewNotificationsServer(pool *pgxpool.Pool, cfg *Config, es *ESClient) *NotificationsServer {
	return &NotificationsServer{pool: pool, cfg: cfg, metrics: newNotifMetrics(), es: es}
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

func locationToProto(loc *LocationRecord) *pb.LocationRecord {
	if loc == nil {
		return nil
	}
	p := &pb.LocationRecord{
		Id:         loc.ID,
		UserId:     loc.UserID,
		Latitude:   loc.Latitude,
		Longitude:  loc.Longitude,
		Source:     loc.Source,
		RecordedAt: timestamppb.New(loc.RecordedAt.UTC()),
	}
	if loc.LiveMessageID != nil {
		p.LiveMessageId = *loc.LiveMessageID
	}
	if loc.Accuracy != nil {
		p.Accuracy = *loc.Accuracy
	}
	if loc.Altitude != nil {
		p.Altitude = *loc.Altitude
	}
	if loc.Heading != nil {
		p.Heading = *loc.Heading
	}
	if loc.Speed != nil {
		p.Speed = *loc.Speed
	}
	return p
}

func ptrFloat64(v float64) *float64 { return &v }

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

func (s *NotificationsServer) CreateReminder(ctx context.Context, req *pb.CreateReminderRequest) (resp *pb.ReminderResponse, err error) {
	defer s.recordRPC(ctx, "CreateReminder", &err)
	log := applog.With(ctx, logger)

	params := scheduleParamsToMap(req.ScheduleParams)

	// Compute initial next fire
	tzOffset := paramInt(params, "tz_offset", s.cfg.TimezoneOffsetHours)
	nowUTC := time.Now().UTC()

	var nextFireAt time.Time
	if req.ScheduleType == "once" {
		once := req.GetScheduleParams().GetOnce()
		dateStr := ""
		if once != nil {
			dateStr = once.Date
		}
		hour := paramInt(params, "hour", 9)
		minute := paramInt(params, "minute", 0)
		loc := time.FixedZone("tz", tzOffset*3600)
		d, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			nextFireAt = nowUTC
		} else {
			nextFireAt = time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc).UTC()
		}
	} else {
		next := ComputeNextFire(ctx, req.ScheduleType, params, nowUTC, tzOffset)
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

func (s *NotificationsServer) StoreLocation(ctx context.Context, req *pb.StoreLocationRequest) (resp *pb.LocationResponse, err error) {
	defer s.recordRPC(ctx, "StoreLocation", &err)
	log := applog.With(ctx, logger)

	loc := &LocationRecord{
		UserID:     req.UserId,
		Latitude:   req.Latitude,
		Longitude:  req.Longitude,
		Source:     req.Source,
		RecordedAt: time.Now().UTC(),
	}
	if req.Accuracy != 0 {
		loc.Accuracy = ptrFloat64(req.Accuracy)
	}
	if req.Altitude != 0 {
		loc.Altitude = ptrFloat64(req.Altitude)
	}
	if req.Heading != 0 {
		loc.Heading = ptrFloat64(req.Heading)
	}
	if req.Speed != 0 {
		loc.Speed = ptrFloat64(req.Speed)
	}
	if req.LiveMessageId != 0 {
		loc.LiveMessageID = &req.LiveMessageId
	}

	stored, err := StoreLocation(ctx, s.pool, loc)
	if err != nil {
		log.Error("store location", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.metrics.RecordLocationUpdate(ctx, req.Source)
	s.metrics.ObserveLatestLocation(req.Latitude, req.Longitude)

	if s.es != nil {
		go func() {
			if err := s.es.IndexLocation(context.Background(), stored); err != nil {
				logger.Warn("es index location failed", zap.Error(err))
			}
		}()
	}

	return &pb.LocationResponse{Success: true, Location: locationToProto(stored)}, nil
}

func (s *NotificationsServer) GetLatestLocation(ctx context.Context, req *pb.GetLatestLocationRequest) (resp *pb.LocationResponse, err error) {
	defer s.recordRPC(ctx, "GetLatestLocation", &err)

	loc, err := GetLatestLocation(ctx, s.pool, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.LocationResponse{Success: true, Location: locationToProto(loc)}, nil
}

func (s *NotificationsServer) GetLocationHistory(ctx context.Context, req *pb.GetLocationHistoryRequest) (resp *pb.LocationHistoryResponse, err error) {
	defer s.recordRPC(ctx, "GetLocationHistory", &err)

	limit := int(req.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	locs, err := GetLocationHistory(ctx, s.pool, req.UserId, limit, offset)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	result := make([]*pb.LocationRecord, len(locs))
	for i, loc := range locs {
		result[i] = locationToProto(loc)
	}
	return &pb.LocationHistoryResponse{Locations: result}, nil
}

func (s *NotificationsServer) ToggleLocationTracking(ctx context.Context, req *pb.ToggleLocationTrackingRequest) (resp *pb.LocationTrackingResponse, err error) {
	defer s.recordRPC(ctx, "ToggleLocationTracking", &err)
	log := applog.With(ctx, logger)

	if err := SetLocationTracking(ctx, s.pool, req.UserId, req.Active); err != nil {
		log.Error("toggle location tracking", zap.Error(err))
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.LocationTrackingResponse{IsActive: req.Active}, nil
}

func (s *NotificationsServer) GetLocationTrackingStatus(ctx context.Context, req *pb.GetLocationTrackingRequest) (resp *pb.LocationTrackingResponse, err error) {
	defer s.recordRPC(ctx, "GetLocationTrackingStatus", &err)

	isActive, err := GetLocationTracking(ctx, s.pool, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.LocationTrackingResponse{IsActive: isActive}, nil
}
