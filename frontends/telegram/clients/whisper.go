package clients

import (
	"context"
	"fmt"
	"io"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"notes-bot/internal/telemetry"
	pb "notes-bot/proto/whisper"
)

const (
	maxMsgSize    = 50 * 1024 * 1024 // 50 MB
	chunkSize     = 1 * 1024 * 1024  // 1 MB per chunk
	pollInterval  = 5 * time.Second
	pollDeadline  = 3 * time.Hour
	submitTimeout = 120 * time.Second
	statusTimeout = 10 * time.Second
)

// JobResult holds the outcome of an async transcription job poll.
type JobResult struct {
	Status          pb.JobStatus
	Text            string
	Error           string
	ProgressPercent float32
}

type WhisperClient struct {
	conn *grpc.ClientConn
	stub pb.TranscriptionServiceClient
}

func NewWhisperClient(ctx context.Context, host, port string) (*WhisperClient, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("dial whisper: %w", err)
	}
	return &WhisperClient{conn: conn, stub: pb.NewTranscriptionServiceClient(conn)}, nil
}

func (c *WhisperClient) Close() {
	c.conn.Close()
}

// Submit uploads audio and returns a job ID and queue position immediately.
func (c *WhisperClient) Submit(ctx context.Context, r io.Reader, format, preset string) (jobID string, queuePosition int, err error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	stream, err := c.stub.Submit(ctx)
	if err != nil {
		if isUnavailable(err) {
			return "", 0, errUnavailable("whisper")
		}
		return "", 0, fmt.Errorf("open submit stream: %w", err)
	}

	if err := sendChunks(stream, r, format, preset); err != nil {
		return "", 0, err
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		if isUnavailable(err) {
			return "", 0, errUnavailable("whisper")
		}
		return "", 0, fmt.Errorf("close submit stream: %w", err)
	}
	return resp.JobId, int(resp.QueuePosition), nil
}

// GetStatus polls the status of a submitted job.
func (c *WhisperClient) GetStatus(ctx context.Context, jobID string) (*JobResult, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	resp, err := c.stub.GetStatus(ctx, &pb.StatusRequest{JobId: jobID})
	if err != nil {
		if isUnavailable(err) {
			return nil, errUnavailable("whisper")
		}
		return nil, fmt.Errorf("get status: %w", err)
	}

	return &JobResult{
		Status:          resp.Status,
		Text:            resp.Text,
		Error:           resp.Error,
		ProgressPercent: resp.ProgressPercent,
	}, nil
}

// Cancel requests cancellation of a job.
func (c *WhisperClient) Cancel(ctx context.Context, jobID string) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	resp, err := c.stub.Cancel(ctx, &pb.CancelRequest{JobId: jobID})
	if err != nil {
		if isUnavailable(err) {
			return false, errUnavailable("whisper")
		}
		return false, fmt.Errorf("cancel job: %w", err)
	}
	return resp.Cancelled, nil
}

func sendChunks(stream interface {
	Send(*pb.TranscribeChunk) error
}, r io.Reader, format, preset string) error {
	buf := make([]byte, chunkSize)
	first := true
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := &pb.TranscribeChunk{Data: buf[:n]}
			if first {
				chunk.Format = format
				chunk.Options = &pb.TranscriptionOptions{Preset: preset}
				first = false
			}
			if sendErr := stream.Send(chunk); sendErr != nil {
				return fmt.Errorf("send chunk: %w", sendErr)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read audio: %w", err)
		}
	}
}
