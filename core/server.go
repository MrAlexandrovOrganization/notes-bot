package core

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"notes_bot/internal/telemetry"
	pb "notes_bot/proto/notes"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type NotesServer struct {
	pb.UnimplementedNotesServiceServer
	calendar CalendarStore
	notes    NoteStore
	ratings  RatingStore
	tasks    TaskStore

	rpcRequests   metric.Int64Counter
	noteFileOps   metric.Int64Counter
	ratingUpdates metric.Int64Counter
}

func NewNotesServer(cal CalendarStore, notes NoteStore, ratings RatingStore, tasks TaskStore) *NotesServer {
	meter := otel.GetMeterProvider().Meter("core")
	rpcRequests, _ := meter.Int64Counter("core.rpc.requests",
		metric.WithDescription("Total gRPC requests by method and status"),
	)
	noteFileOps, _ := meter.Int64Counter("core.note.file.operations",
		metric.WithDescription("Total note file operations by type"),
	)
	ratingUpdates, _ := meter.Int64Counter("core.rating.updates",
		metric.WithDescription("Total rating updates"),
	)
	return &NotesServer{
		calendar:      cal,
		notes:         notes,
		ratings:       ratings,
		tasks:         tasks,
		rpcRequests:   rpcRequests,
		noteFileOps:   noteFileOps,
		ratingUpdates: ratingUpdates,
	}
}

func NewDefaultNotesServer() *NotesServer {
	return NewNotesServer(&realCalendarStore{}, &realNoteStore{}, &realRatingStore{}, &realTaskStore{})
}

func (s *NotesServer) recordRPC(ctx context.Context, method string, err *error) {
	if s.rpcRequests == nil {
		return
	}
	st := "ok"
	if *err != nil {
		st = "error"
	}
	s.rpcRequests.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("method", method),
			attribute.String("status", st),
		),
	)
}

func (s *NotesServer) GetTodayDate(ctx context.Context, req *pb.Empty) (resp *pb.DateResponse, err error) {
	defer s.recordRPC(ctx, "GetTodayDate", &err)
	return &pb.DateResponse{Date: s.calendar.TodayDate(ctx)}, nil
}

func (s *NotesServer) GetExistingDates(ctx context.Context, req *pb.Empty) (resp *pb.ExistingDatesResponse, err error) {
	defer s.recordRPC(ctx, "GetExistingDates", &err)

	_, span := telemetry.StartSpan(ctx)
	defer span.End()

	dates, err := s.calendar.GetExistingDates(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get dates: %v", err)
	}
	return &pb.ExistingDatesResponse{Dates: dates}, nil
}

func (s *NotesServer) EnsureNote(ctx context.Context, req *pb.DateRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "EnsureNote", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	if err := s.notes.EnsureNote(ctx, req.Date); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create note: %v", err)
	}
	if s.noteFileOps != nil {
		s.noteFileOps.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "ensure")))
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) GetNote(ctx context.Context, req *pb.DateRequest) (resp *pb.NoteResponse, err error) {
	defer s.recordRPC(ctx, "GetNote", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	content, err := s.notes.ReadNote(ctx, req.Date)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read note: %v", err)
	}
	if content == "" {
		return nil, status.Errorf(codes.NotFound, "note not found for date: %s", req.Date)
	}
	if s.noteFileOps != nil {
		s.noteFileOps.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "get")))
	}
	return &pb.NoteResponse{Content: content}, nil
}

func (s *NotesServer) GetRating(ctx context.Context, req *pb.DateRequest) (resp *pb.RatingResponse, err error) {
	defer s.recordRPC(ctx, "GetRating", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	content, err := s.notes.ReadNote(ctx, req.Date)
	if err != nil || content == "" {
		return &pb.RatingResponse{HasRating: false}, nil
	}
	rating := s.ratings.GetRating(ctx, content)
	if rating == nil {
		return &pb.RatingResponse{HasRating: false}, nil
	}
	return &pb.RatingResponse{HasRating: true, Rating: int32(*rating)}, nil
}

func (s *NotesServer) UpdateRating(ctx context.Context, req *pb.UpdateRatingRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "UpdateRating", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	if err := s.ratings.UpdateRating(ctx, req.Date, int(req.Rating)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update rating: %v", err)
	}
	if s.ratingUpdates != nil {
		s.ratingUpdates.Add(ctx, 1)
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) GetTasks(ctx context.Context, req *pb.DateRequest) (resp *pb.TasksResponse, err error) {
	defer s.recordRPC(ctx, "GetTasks", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	content, err := s.notes.ReadNote(ctx, req.Date)
	if err != nil || content == "" {
		return &pb.TasksResponse{Tasks: []*pb.Task{}}, nil
	}
	rawTasks := s.tasks.ParseTasks(ctx, content)
	tasks := make([]*pb.Task, len(rawTasks))
	for i, t := range rawTasks {
		tasks[i] = &pb.Task{
			Text:       t.Text,
			Completed:  t.Completed,
			Index:      int32(t.Index),
			LineNumber: int32(t.LineNumber),
		}
	}
	return &pb.TasksResponse{Tasks: tasks}, nil
}

func (s *NotesServer) ToggleTask(ctx context.Context, req *pb.ToggleTaskRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "ToggleTask", &err)

	_, span := telemetry.StartSpan(ctx,
		attribute.String("note.date", req.Date),
		attribute.Int("task.index", int(req.TaskIndex)))
	defer span.End()

	if err := s.tasks.ToggleTask(ctx, req.Date, int(req.TaskIndex)); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to toggle task: %v", err)
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) AddTask(ctx context.Context, req *pb.AddTaskRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "AddTask", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	if err := s.tasks.AddTask(ctx, req.Date, req.TaskText); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add task: %v", err)
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) AppendToNote(ctx context.Context, req *pb.AppendRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "AppendToNote", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.date", req.Date))
	defer span.End()

	if err := s.notes.AppendToNote(ctx, req.Date, req.Text); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to append to note: %v", err)
	}
	if s.noteFileOps != nil {
		s.noteFileOps.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "append")))
	}
	return &pb.SuccessResponse{Success: true}, nil
}
