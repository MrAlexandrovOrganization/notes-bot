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
	requests     []*pb.AppendRequest
	pathRequests []*pb.AppendByPathRequest
	err          error
}

func (s *appendOnceStub) AppendToNote(_ context.Context, request *pb.AppendRequest, _ ...grpc.CallOption) (*pb.SuccessResponse, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return nil, s.err
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func (s *appendOnceStub) AppendToNoteByPath(_ context.Context, request *pb.AppendByPathRequest, _ ...grpc.CallOption) (*pb.SuccessResponse, error) {
	s.pathRequests = append(s.pathRequests, request)
	if s.err != nil {
		return nil, s.err
	}
	return &pb.SuccessResponse{Success: true}, nil
}

func TestCoreClientAppendByPathOnceKeepsDestinationAndKey(t *testing.T) {
	stub := &appendOnceStub{err: status.Error(codes.Unavailable, "synthetic lost response")}
	client := &CoreClient{stub: stub}
	_, err := client.AppendToNoteByPathOnce(t.Context(), "Films/First.md", "review", "voice:one")
	require.Equal(t, codes.Unavailable, status.Code(err))
	stub.err = nil
	ok, err := client.AppendToNoteByPathOnce(t.Context(), "Films/First.md", "review", "voice:one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, stub.pathRequests, 2)
	require.Equal(t, stub.pathRequests[0], stub.pathRequests[1])
	require.Equal(t, "Films/First.md", stub.pathRequests[1].Relpath)
	require.Equal(t, "voice:one", stub.pathRequests[1].GetOperationId())
	_, err = client.AppendToNoteByPathOnce(t.Context(), "Films/First.md", "review", "")
	require.Error(t, err)
	require.Len(t, stub.pathRequests, 2)
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
