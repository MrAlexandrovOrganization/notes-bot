package clients

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/notes"
)

type appendOnceStub struct {
	pb.NotesServiceClient
	requests []*pb.AppendRequest
	err      error
}

func (s *appendOnceStub) AppendToNote(_ context.Context, request *pb.AppendRequest, _ ...grpc.CallOption) (*pb.SuccessResponse, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return nil, s.err
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func TestCoreClientAppendOnceCarriesStableKeyAndErrors(t *testing.T) {
	stub := &appendOnceStub{err: status.Error(codes.Unavailable, "synthetic lost response")}
	client := &CoreClient{stub: stub}
	ok, err := client.AppendToNoteOnce(t.Context(), "12-Oct-2026", "synthetic text", "voice:one")
	require.Equal(t, codes.Unavailable, status.Code(err))
	assert.False(t, ok)
	stub.err = nil
	ok, err = client.AppendToNoteOnce(t.Context(), "12-Oct-2026", "synthetic text", "voice:one")
	require.NoError(t, err)
	assert.True(t, ok)
	require.Len(t, stub.requests, 2)
	assert.Equal(t, stub.requests[0], stub.requests[1])
	assert.Equal(t, "voice:one", stub.requests[1].GetOperationId())
	_, err = client.AppendToNoteOnce(t.Context(), "12-Oct-2026", "synthetic text", "")
	assert.Error(t, err)
	require.Len(t, stub.requests, 2)
	_, err = client.AppendToNote(t.Context(), "12-Oct-2026", "ordinary")
	require.NoError(t, err)
	assert.Nil(t, stub.requests[2].OperationId)
}
