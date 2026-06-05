package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/internal/grpcutil"
	pb "notes-bot/proto/notifications"
)

type ReminderInfo struct {
	ID           int64
	UserID       int64
	Title        string
	ScheduleType string
	NextFireAt   time.Time
	IsActive     bool
	CreateTask   bool
}

type NotificationsClient struct {
	conn *grpc.ClientConn
	stub pb.NotificationsServiceClient
}

func NewNotificationsClient(host, port string) (*NotificationsClient, error) {
	conn, err := grpcutil.Dial(host, port)
	if err != nil {
		return nil, fmt.Errorf("dial notifications: %w", err)
	}
	return &NotificationsClient{conn: conn, stub: pb.NewNotificationsServiceClient(conn)}, nil
}

func (c *NotificationsClient) Close() {
	c.conn.Close()
}

func isUnavailable(err error) bool {
	if st, ok := status.FromError(err); ok {
		code := st.Code()
		return code == codes.Unavailable || code == codes.DeadlineExceeded
	}
	return false
}

func (c *NotificationsClient) CreateReminder(ctx context.Context,
	userID int64, title, scheduleType string, scheduleParams *pb.ScheduleParams, createTask bool,
) (*ReminderInfo, error) {
	resp, err := c.stub.CreateReminder(ctx, &pb.CreateReminderRequest{
		UserId:         userID,
		Title:          title,
		ScheduleType:   scheduleType,
		ScheduleParams: scheduleParams,
		CreateTask:     createTask,
	})
	if err != nil {
		if isUnavailable(err) {
			return nil, errUnavailable("notifications")
		}
		return nil, err
	}
	if !resp.Success {
		return nil, nil
	}
	return protoToReminderInfo(resp.Reminder), nil
}

func (c *NotificationsClient) ListReminders(ctx context.Context, userID int64) ([]*ReminderInfo, error) {
	resp, err := c.stub.ListReminders(ctx, &pb.ListRemindersRequest{UserId: userID})
	if err != nil {
		if isUnavailable(err) {
			return nil, errUnavailable("notifications")
		}
		return nil, err
	}
	result := make([]*ReminderInfo, len(resp.Reminders))
	for i, r := range resp.Reminders {
		result[i] = protoToReminderInfo(r)
	}
	return result, nil
}

func (c *NotificationsClient) DeleteReminder(ctx context.Context, reminderID, userID int64) (bool, error) {
	resp, err := c.stub.DeleteReminder(ctx, &pb.DeleteReminderRequest{
		ReminderId: reminderID,
		UserId:     userID,
	})
	if err != nil {
		if isUnavailable(err) {
			return false, errUnavailable("notifications")
		}
		return false, err
	}
	return resp.Success, nil
}

func (c *NotificationsClient) PostponeReminder(ctx context.Context,
	reminderID, userID int64, postponeMinutes int32,
) (*ReminderInfo, error) {
	resp, err := c.stub.PostponeReminder(ctx, &pb.PostponeReminderRequest{
		ReminderId:      reminderID,
		UserId:          userID,
		PostponeMinutes: postponeMinutes,
	})
	if err != nil {
		if isUnavailable(err) {
			return nil, errUnavailable("notifications")
		}
		return nil, err
	}
	if !resp.Success {
		return nil, nil
	}
	return protoToReminderInfo(resp.Reminder), nil
}

func protoToReminderInfo(r *pb.Reminder) *ReminderInfo {
	if r == nil {
		return nil
	}
	var nextFireAt time.Time
	if r.NextFireAt != nil {
		nextFireAt = r.NextFireAt.AsTime()
	}
	return &ReminderInfo{
		ID:           r.Id,
		UserID:       r.UserId,
		Title:        r.Title,
		ScheduleType: r.ScheduleType,
		NextFireAt:   nextFireAt,
		IsActive:     r.IsActive,
		CreateTask:   r.CreateTask,
	}
}
