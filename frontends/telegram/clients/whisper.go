package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "notes_bot/proto/whisper"
)

const maxMsgSize = 50 * 1024 * 1024 // 50 MB

type WhisperUnavailableError struct{}

func (e *WhisperUnavailableError) Error() string {
	return "whisper service unavailable"
}

type WhisperClient struct {
	conn *grpc.ClientConn
	stub pb.TranscriptionServiceClient
}

func NewWhisperClient(host, port string) (*WhisperClient, error) {
	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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

func (c *WhisperClient) Transcribe(ctx context.Context, audioData []byte, format string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := c.stub.Transcribe(timeoutCtx, &pb.TranscribeRequest{
		AudioData: audioData,
		Format:    format,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			if st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
				return "", &WhisperUnavailableError{}
			}
		}
		return "", err
	}
	return resp.Text, nil
}
