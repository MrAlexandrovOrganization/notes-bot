package clients

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"notes-bot/internal/telemetry"
	pb "notes-bot/proto/notes"
)

const grpcTimeout = 10 * time.Second

type Task struct {
	Text       string
	Completed  bool
	Index      int
	LineNumber int
}

type CoreClient struct {
	conn *grpc.ClientConn
	stub pb.NotesServiceClient
}

func NewCoreClient(ctx context.Context, host, port string) (*CoreClient, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial core: %w", err)
	}
	return &CoreClient{conn: conn, stub: pb.NewNotesServiceClient(conn)}, nil
}

func (c *CoreClient) Close() {
	c.conn.Close()
}

func (c *CoreClient) GetTodayDate(ctx context.Context) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.GetTodayDate(ctx, &pb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Date, nil
}

func (c *CoreClient) GetExistingDates(ctx context.Context) ([]string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.GetExistingDates(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.Dates, nil
}

func (c *CoreClient) EnsureNote(ctx context.Context, date string) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.EnsureNote(ctx, &pb.DateRequest{Date: date})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *CoreClient) GetNote(ctx context.Context, date string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.GetNote(ctx, &pb.DateRequest{Date: date})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return "", nil
		}
		return "", err
	}
	return resp.Content, nil
}

func (c *CoreClient) GetRating(ctx context.Context, date string) (int, bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.GetRating(ctx, &pb.DateRequest{Date: date})
	if err != nil {
		return 0, false, err
	}
	return int(resp.Rating), resp.HasRating, nil
}

func (c *CoreClient) UpdateRating(ctx context.Context, date string, rating int) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.UpdateRating(ctx, &pb.UpdateRatingRequest{Date: date, Rating: int32(rating)})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *CoreClient) GetTasks(ctx context.Context, date string) ([]*Task, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.GetTasks(ctx, &pb.DateRequest{Date: date})
	if err != nil {
		return nil, err
	}
	tasks := make([]*Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		tasks[i] = &Task{
			Text:       t.Text,
			Completed:  t.Completed,
			Index:      int(t.Index),
			LineNumber: int(t.LineNumber),
		}
	}
	return tasks, nil
}

func (c *CoreClient) ToggleTask(ctx context.Context, date string, taskIndex int) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.ToggleTask(ctx, &pb.ToggleTaskRequest{Date: date, TaskIndex: int32(taskIndex)})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *CoreClient) AddTask(ctx context.Context, date, taskText string) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.AddTask(ctx, &pb.AddTaskRequest{Date: date, TaskText: taskText})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *CoreClient) AppendToNote(ctx context.Context, date, text string) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, grpcTimeout)
	defer cancel()
	resp, err := c.stub.AppendToNote(ctx, &pb.AppendRequest{Date: date, Text: text})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}
