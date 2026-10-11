package clients

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"

	"notes-bot/internal/grpcutil"
	pb "notes-bot/proto/whisper"
)

const (
	maxMsgSize    = 50 * 1024 * 1024 // 50 MB
	chunkSize     = 1 * 1024 * 1024  // 1 MB per chunk
	pollInterval  = 5 * time.Second
	pollDeadline  = 3 * time.Hour
	submitTimeout = 120 * time.Second
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

func NewWhisperClient(host, port string) (*WhisperClient, error) {
	conn, err := grpcutil.Dial(host, port,
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
// Uses a manual timeout because Submit is a client-streaming RPC — unary interceptors do not apply.
func (c *WhisperClient) Submit(ctx context.Context, r io.Reader, format, preset string) (jobID string, queuePosition int, err error) {
	return c.submit(ctx, r, format, preset, "")
}

func (c *WhisperClient) SubmitOnce(ctx context.Context, r io.Reader, format, preset, requestID string) (string, int, error) {
	if requestID == "" {
		return "", 0, fmt.Errorf("request ID is required")
	}
	return c.submit(ctx, r, format, preset, requestID)
}

func (c *WhisperClient) submit(ctx context.Context, r io.Reader, format, preset, requestID string) (jobID string, queuePosition int, err error) {
	ctx, cancel := context.WithTimeout(ctx, submitTimeout)
	defer cancel()

	stream, err := c.stub.Submit(ctx)
	if err != nil {
		if isUnavailable(err) {
			return "", 0, errUnavailable("whisper")
		}
		return "", 0, fmt.Errorf("open submit stream: %w", err)
	}

	resp, err := uploadAndReceive(stream, r, format, preset, requestID)
	if err != nil {
		if isUnavailable(err) {
			return "", 0, errUnavailable("whisper")
		}
		return "", 0, fmt.Errorf("close submit stream: %w", err)
	}
	return resp.JobId, int(resp.QueuePosition), nil
}

// A replay can finish on the first chunk, before the client has streamed the
// rest of the file. Send then returns EOF; the successful response still needs
// to be read. Other reader/send failures must not be hidden.
func uploadAndReceive(stream interface {
	Send(*pb.TranscribeChunk) error
	CloseAndRecv() (*pb.SubmitResponse, error)
}, r io.Reader, format, preset, requestID string) (*pb.SubmitResponse, error) {
	if err := sendDurableChunks(stream, r, format, preset, requestID); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return stream.CloseAndRecv()
}

func (c *WhisperClient) Ack(ctx context.Context, jobID string) (bool, error) {
	resp, err := c.stub.Ack(ctx, &pb.AckRequest{JobId: jobID})
	if err != nil {
		return false, fmt.Errorf("ack transcription: %w", err)
	}
	return resp.Acknowledged, nil
}

// GetStatus polls the status of a submitted job.
func (c *WhisperClient) GetStatus(ctx context.Context, jobID string) (*JobResult, error) {
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
	return sendDurableChunks(stream, r, format, preset, "")
}

func sendDurableChunks(stream interface {
	Send(*pb.TranscribeChunk) error
}, r io.Reader, format, preset, requestID string) error {
	buf := make([]byte, chunkSize)
	first := true
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := &pb.TranscribeChunk{Data: buf[:n]}
			if first {
				chunk.Format = format
				chunk.Options = &pb.TranscriptionOptions{Preset: preset}
				chunk.RequestId = requestID
				chunk.RetainUntilAck = requestID != ""
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
