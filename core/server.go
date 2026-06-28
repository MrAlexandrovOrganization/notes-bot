package core

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"golang.org/x/sync/errgroup"

	"notes-bot/internal/telemetry"
	pb "notes-bot/proto/notes"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
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
	ratingValue   metric.Int64Gauge
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
	ratingValue, _ := meter.Int64Gauge("core.rating.value",
		metric.WithDescription("Last recorded day rating (0-10)"),
	)
	return &NotesServer{
		calendar:      cal,
		notes:         notes,
		ratings:       ratings,
		tasks:         tasks,
		rpcRequests:   rpcRequests,
		noteFileOps:   noteFileOps,
		ratingUpdates: ratingUpdates,
		ratingValue:   ratingValue,
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

func (s *NotesServer) GetTodayDate(ctx context.Context, req *emptypb.Empty) (resp *pb.DateResponse, err error) {
	defer s.recordRPC(ctx, "GetTodayDate", &err)
	return &pb.DateResponse{Date: s.calendar.TodayDate(ctx)}, nil
}

func (s *NotesServer) GetExistingDates(ctx context.Context, req *emptypb.Empty) (resp *pb.ExistingDatesResponse, err error) {
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
	if s.ratingValue != nil {
		s.ratingValue.Record(ctx, int64(req.Rating), metric.WithAttributes(attribute.String("date", toISODate(req.Date))))
	}
	return &pb.SuccessResponse{Success: true}, nil
}

// toISODate converts "DD-Mmm-YYYY" to "YYYY-MM-DD" for correct chronological sorting in Grafana.
// Returns the original string if parsing fails.
func toISODate(date string) string {
	t, err := time.Parse("02-Jan-2006", date)
	if err != nil {
		return date
	}
	return t.Format("2006-01-02")
}

// LoadHistoricalRatings scans all existing notes and records their ratings as metrics.
// Should be called once after server startup.
func (s *NotesServer) LoadHistoricalRatings(ctx context.Context) {
	if s.ratingValue == nil {
		return
	}
	dates, err := s.calendar.GetExistingDates(ctx)
	if err != nil {
		return
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(6)

	for _, date := range dates {
		g.Go(func() error {
			content, err := s.notes.ReadNote(gctx, date)
			if err != nil || content == "" {
				return nil
			}
			rating := s.ratings.GetRating(gctx, content)
			if rating == nil {
				return nil
			}
			s.ratingValue.Record(gctx, int64(*rating), metric.WithAttributes(attribute.String("date", toISODate(date))))
			return nil
		})
	}

	g.Wait() //nolint:errcheck
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

func (s *NotesServer) AppendToNoteByPath(ctx context.Context, req *pb.AppendByPathRequest) (resp *pb.SuccessResponse, err error) {
	defer s.recordRPC(ctx, "AppendToNoteByPath", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.relpath", req.Relpath))
	defer span.End()

	if err := s.notes.AppendByPath(ctx, req.Relpath, req.Text); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to append to note: %v", err)
	}
	if s.noteFileOps != nil {
		s.noteFileOps.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "append_by_path")))
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *NotesServer) ListDirectory(ctx context.Context, req *pb.ListDirectoryRequest) (resp *pb.ListDirectoryResponse, err error) {
	defer s.recordRPC(ctx, "ListDirectory", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("browse.relpath", req.Relpath))
	defer span.End()

	entries, err := s.notes.ListDirectory(ctx, req.Relpath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list directory: %v", err)
	}
	pbEntries := make([]*pb.DirEntry, len(entries))
	for i, e := range entries {
		pbEntries[i] = &pb.DirEntry{
			Name:    e.Name,
			Relpath: e.Relpath,
			IsDir:   e.IsDir,
		}
	}
	return &pb.ListDirectoryResponse{Entries: pbEntries}, nil
}

func (s *NotesServer) GetNoteByPath(ctx context.Context, req *pb.GetNoteByPathRequest) (resp *pb.NoteResponse, err error) {
	defer s.recordRPC(ctx, "GetNoteByPath", &err)

	_, span := telemetry.StartSpan(ctx, attribute.String("note.relpath", req.Relpath))
	defer span.End()

	content, err := s.notes.ReadNoteByPath(ctx, req.Relpath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read note: %v", err)
	}
	if content == "" {
		return nil, status.Errorf(codes.NotFound, "note not found: %s", req.Relpath)
	}
	return &pb.NoteResponse{Content: content}, nil
}
