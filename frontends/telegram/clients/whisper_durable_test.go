package clients

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/whisper"
)

type durableUploadStream struct {
	chunks     []*pb.TranscribeChunk
	sendError  error
	failSendAt int
	response   *pb.SubmitResponse
	closeError error
	closeCalls int
}

func (s *durableUploadStream) Send(chunk *pb.TranscribeChunk) error {
	// Send may reuse the audio buffer on the next read.
	copyChunk := &pb.TranscribeChunk{
		Data:           bytes.Clone(chunk.GetData()),
		Format:         chunk.GetFormat(),
		Options:        chunk.GetOptions(),
		RequestId:      chunk.GetRequestId(),
		RetainUntilAck: chunk.GetRetainUntilAck(),
	}
	s.chunks = append(s.chunks, copyChunk)
	if len(s.chunks) == s.failSendAt {
		return s.sendError
	}
	return nil
}

func (s *durableUploadStream) CloseAndRecv() (*pb.SubmitResponse, error) {
	s.closeCalls++
	return s.response, s.closeError
}

type durableAudioReadError struct{ err error }

func (r durableAudioReadError) Read([]byte) (int, error) { return 0, r.err }

func TestDurableUploadReplayReadsEarlyResponse(t *testing.T) {
	// The existing job finishes Submit after the first chunk. gRPC reports EOF
	// on a later Send even though CloseAndRecv still holds a successful reply.
	audio := bytes.Repeat([]byte{42}, 3*chunkSize+17)
	stream := &durableUploadStream{
		failSendAt: 2,
		sendError:  io.EOF,
		response:   &pb.SubmitResponse{JobId: "original-job", QueuePosition: 7},
	}
	response, err := uploadAndReceive(stream, bytes.NewReader(audio), "ogg", "voice", "notes-bot:persisted-id")
	require.NoError(t, err)
	assert.Equal(t, "original-job", response.GetJobId())
	assert.Equal(t, int32(7), response.GetQueuePosition())
	assert.Equal(t, 1, stream.closeCalls)
	require.Len(t, stream.chunks, 2)
	assert.Len(t, stream.chunks[0].GetData(), chunkSize)
	assert.Equal(t, "notes-bot:persisted-id", stream.chunks[0].GetRequestId())
	assert.True(t, stream.chunks[0].GetRetainUntilAck())
	assert.Equal(t, "ogg", stream.chunks[0].GetFormat())
	assert.Equal(t, "voice", stream.chunks[0].GetOptions().GetPreset())
	assert.Empty(t, stream.chunks[1].GetRequestId())
	assert.False(t, stream.chunks[1].GetRetainUntilAck())
}

func TestDurableUploadPreservesReaderAndSendErrors(t *testing.T) {
	syntheticError := errors.New("synthetic upload failure")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		stream durableUploadStream
	}{
		{
			name:   "reader after first chunk",
			reader: io.MultiReader(bytes.NewReader([]byte("audio")), durableAudioReadError{err: syntheticError}),
		},
		{
			name:   "send after first chunk",
			reader: bytes.NewReader(bytes.Repeat([]byte{42}, 3*chunkSize)),
			stream: durableUploadStream{failSendAt: 2, sendError: syntheticError},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := uploadAndReceive(&tc.stream, tc.reader, "ogg", "voice", "notes-bot:persisted-id")
			require.ErrorIs(t, err, syntheticError)
			assert.Nil(t, response)
			assert.Zero(t, tc.stream.closeCalls)
		})
	}
}

func TestDurableUploadReplayPreservesReceiveError(t *testing.T) {
	syntheticError := errors.New("synthetic failed response")
	stream := &durableUploadStream{failSendAt: 2, sendError: io.EOF, closeError: syntheticError}
	response, err := uploadAndReceive(stream, bytes.NewReader(bytes.Repeat([]byte{42}, 3*chunkSize)), "ogg", "voice", "notes-bot:persisted-id")
	require.ErrorIs(t, err, syntheticError)
	assert.Nil(t, response)
	assert.Equal(t, 1, stream.closeCalls)
}
