package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "notes_bot/proto/notifications"
)

type ReminderInfo struct {
	ID                 int64
	UserID             int64
	Title              string
	ScheduleType       string
	ScheduleParamsJSON string
	NextFireAt         string
	IsActive           bool
	CreateTask         bool
}

type NotificationsClient struct {
	conn *grpc.ClientConn
	stub pb.NotificationsServiceClient
}

func NewNotificationsClient(host, port string) (*NotificationsClient, error) {
	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	userID int64, title, scheduleType, scheduleParamsJSON string, createTask bool,
) (*ReminderInfo, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.stub.CreateReminder(timeoutCtx, &pb.CreateReminderRequest{
		UserId:             userID,
		Title:              title,
		ScheduleType:       scheduleType,
		ScheduleParamsJson: scheduleParamsJSON,
		CreateTask:         createTask,
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
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.stub.ListReminders(timeoutCtx, &pb.ListRemindersRequest{UserId: userID})
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
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.stub.DeleteReminder(timeoutCtx, &pb.DeleteReminderRequest{
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
	reminderID, userID int64, postponeDays int32, targetDate string, postponeHours int32,
) (*ReminderInfo, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := c.stub.PostponeReminder(timeoutCtx, &pb.PostponeReminderRequest{
		ReminderId:    reminderID,
		UserId:        userID,
		PostponeDays:  postponeDays,
		TargetDate:    targetDate,
		PostponeHours: postponeHours,
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
	return &ReminderInfo{
		ID:                 r.Id,
		UserID:             r.UserId,
		Title:              r.Title,
		ScheduleType:       r.ScheduleType,
		ScheduleParamsJSON: r.ScheduleParamsJson,
		NextFireAt:         r.NextFireAt,
		IsActive:           r.IsActive,
		CreateTask:         r.CreateTask,
	}
}
