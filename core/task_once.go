package core

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/core/features"
	"notes-bot/internal/taskactions"
	pb "notes-bot/proto/notes"
)

type idempotentTaskStore interface {
	AddTaskOnce(ctx context.Context, date, text, operationID string) error
}

func (r *realTaskStore) AddTaskOnce(ctx context.Context, date, text, operationID string) error {
	return (&realNoteStore{}).applyDailyOperation(ctx, date, text, operationID, "task\x00", func(path, content string) (string, error) {
		if err := recoverTaskIntent(ctx, path); err != nil {
			return "", err
		}
		return features.AddTaskContent(ctx, content, text)
	})
}

func (s *NotesServer) AddTaskOnce(ctx context.Context, req *pb.AddTaskOnceRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "AddTaskOnce", &err)
	if err := validateDate(req.GetDate()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := validateOperationID(req.GetOperationId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if !taskactions.ValidTitle(req.GetTaskText()) {
		return nil, status.Error(codes.InvalidArgument, "invalid task text")
	}
	store, ok := s.tasks.(idempotentTaskStore)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "task store does not support idempotent creation")
	}
	err = store.AddTaskOnce(ctx, req.GetDate(), req.GetTaskText(), req.GetOperationId())
	if errors.Is(err, ErrOperationConflict) {
		return nil, status.Error(codes.AlreadyExists, "task operation already used with different input")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to add task")
	}
	return &pb.SuccessResponse{Success: true}, nil
}
