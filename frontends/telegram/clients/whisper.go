package clients

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"notes_bot/internal/telemetry"
	pb "notes_bot/proto/whisper"
)

const (
	maxMsgSize     = 50 * 1024 * 1024   // 50 MB
	chunkSize      = 1 * 1024 * 1024    // 1 MB per chunk
	whisperTimeout = 3600 * time.Second // 1 hour for long lectures
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

// Transcribe sends audio to the shared Whisper service and blocks until the
// transcription is complete. Satisfies clients.WhisperService interface.
func (c *WhisperClient) Transcribe(ctx context.Context, audioData []byte, format string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	timeoutCtx, cancel := context.WithTimeout(ctx, whisperTimeout)
	defer cancel()

	stream, err := c.stub.Transcribe(timeoutCtx)
	if err != nil {
		if isUnavailable(err) {
			return "", errUnavailable("whisper")
		}
		return "", fmt.Errorf("open transcribe stream: %w", err)
	}

	for i := 0; i < len(audioData); i += chunkSize {
		end := i + chunkSize
		if end > len(audioData) {
			end = len(audioData)
		}
		chunk := &pb.TranscribeChunk{Data: audioData[i:end]}
		if i == 0 {
			chunk.Format = format
		}
		if err := stream.Send(chunk); err != nil {
			return "", fmt.Errorf("send chunk: %w", err)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		if isUnavailable(err) {
			return "", errUnavailable("whisper")
		}
		return "", fmt.Errorf("receive transcription: %w", err)
	}
	return resp.Text, nil
}
