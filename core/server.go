package core

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
}

func NewNotesServer() *NotesServer {
	return &NotesServer{
		calendar: &realCalendarStore{},
		notes:    &realNoteStore{},
		ratings:  &realRatingStore{},
		tasks:    &realTaskStore{},
	}
}

func (s *NotesServer) GetTodayDate(ctx context.Context, req *pb.Empty) (*pb.DateResponse, error) {
	return &pb.DateResponse{Date: s.calendar.TodayDate()}, nil
}

func (s *NotesServer) GetExistingDates(ctx context.Context, req *pb.Empty) (*pb.ExistingDatesResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "calendar.GetExistingDates")
	defer span.End()

	dates, err := s.calendar.GetExistingDates()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get dates: %v", err)
	}
	return &pb.ExistingDatesResponse{Dates: dates}, nil
}

func (s *NotesServer) EnsureNote(ctx context.Context, req *pb.DateRequest) (*pb.SuccessResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "note.EnsureNote",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	if err := s.notes.EnsureNote(req.Date); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create note: %v", err)
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) GetNote(ctx context.Context, req *pb.DateRequest) (*pb.NoteResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "note.GetNote",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	content, err := s.notes.ReadNote(req.Date)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read note: %v", err)
	}
	if content == "" {
		return nil, status.Errorf(codes.NotFound, "note not found for date: %s", req.Date)
	}
	return &pb.NoteResponse{Content: content}, nil
}

func (s *NotesServer) GetRating(ctx context.Context, req *pb.DateRequest) (*pb.RatingResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "rating.GetRating",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	content, err := s.notes.ReadNote(req.Date)
	if err != nil || content == "" {
		return &pb.RatingResponse{HasRating: false}, nil
	}
	rating := s.ratings.GetRating(content)
	if rating == nil {
		return &pb.RatingResponse{HasRating: false}, nil
	}
	return &pb.RatingResponse{HasRating: true, Rating: int32(*rating)}, nil
}

func (s *NotesServer) UpdateRating(ctx context.Context, req *pb.UpdateRatingRequest) (*pb.SuccessResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "rating.UpdateRating",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	if err := s.ratings.UpdateRating(req.Date, int(req.Rating)); err != nil {
		return &pb.SuccessResponse{Success: false}, nil
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) GetTasks(ctx context.Context, req *pb.DateRequest) (*pb.TasksResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "tasks.GetTasks",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	content, err := s.notes.ReadNote(req.Date)
	if err != nil || content == "" {
		return &pb.TasksResponse{Tasks: []*pb.Task{}}, nil
	}
	rawTasks := s.tasks.ParseTasks(content)
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

func (s *NotesServer) ToggleTask(ctx context.Context, req *pb.ToggleTaskRequest) (*pb.SuccessResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "tasks.ToggleTask",
		trace.WithAttributes(
			attribute.String("note.date", req.Date),
			attribute.Int("task.index", int(req.TaskIndex)),
		))
	defer span.End()

	if err := s.tasks.ToggleTask(req.Date, int(req.TaskIndex)); err != nil {
		return &pb.SuccessResponse{Success: false}, nil
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) AddTask(ctx context.Context, req *pb.AddTaskRequest) (*pb.SuccessResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "tasks.AddTask",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	if err := s.tasks.AddTask(req.Date, req.TaskText); err != nil {
		return &pb.SuccessResponse{Success: false}, nil
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) AppendToNote(ctx context.Context, req *pb.AppendRequest) (*pb.SuccessResponse, error) {
	_, span := otel.Tracer("core").Start(ctx, "note.AppendToNote",
		trace.WithAttributes(attribute.String("note.date", req.Date)))
	defer span.End()

	if err := s.notes.AppendToNote(req.Date, req.Text); err != nil {
		logger.Error("error appending to note")
		return &pb.SuccessResponse{Success: false}, nil
	}
	return &pb.SuccessResponse{Success: true}, nil
}
