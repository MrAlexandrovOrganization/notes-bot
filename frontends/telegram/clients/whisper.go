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
	maxMsgSize   = 50 * 1024 * 1024 // 50 MB
	chunkSize    = 1 * 1024 * 1024  // 1 MB per chunk
	pollInterval = 5 * time.Second
	pollDeadline = 3 * time.Hour
)

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

// Transcribe submits audio for transcription and polls until done.
// Satisfies clients.WhisperService interface.
func (c *WhisperClient) Transcribe(ctx context.Context, r io.Reader, format string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, pollDeadline)
	defer cancel()

	jobID, err := c.submit(ctx, r, format)
	if err != nil {
		return "", err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			resp, err := c.stub.GetStatus(ctx, &pb.StatusRequest{JobId: jobID})
			if err != nil {
				if isUnavailable(err) {
					return "", errUnavailable("whisper")
				}
				return "", fmt.Errorf("get status: %w", err)
			}
			switch resp.Status {
			case pb.JobStatus_DONE:
				return resp.Text, nil
			case pb.JobStatus_FAILED:
				return "", fmt.Errorf("transcription failed: %s", resp.Error)
			}
		}
	}
}

func (c *WhisperClient) submit(ctx context.Context, r io.Reader, format string) (string, error) {
	stream, err := c.stub.Submit(ctx)
	if err != nil {
		if isUnavailable(err) {
			return "", errUnavailable("whisper")
		}
		return "", fmt.Errorf("open submit stream: %w", err)
	}

	if err := sendChunks(stream, r, format); err != nil {
		return "", err
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		if isUnavailable(err) {
			return "", errUnavailable("whisper")
		}
		return "", fmt.Errorf("close submit stream: %w", err)
	}
	return resp.JobId, nil
}

func sendChunks(stream interface {
	Send(*pb.TranscribeChunk) error
}, r io.Reader, format string) error {
	buf := make([]byte, chunkSize)
	first := true
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := &pb.TranscribeChunk{Data: buf[:n]}
			if first {
				chunk.Format = format
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
