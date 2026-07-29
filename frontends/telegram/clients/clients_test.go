package clients

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/whisper"
)

// mockStream implements the sendChunks stream interface for testing.
type mockStream struct {
	chunks []*pb.TranscribeChunk
}

func (m *mockStream) Send(c *pb.TranscribeChunk) error {
	m.chunks = append(m.chunks, c)
	return nil
}

func TestSendChunks_SingleChunk(t *testing.T) {
	data := []byte("audio data here")
	stream := &mockStream{}
	err := sendChunks(stream, &bytesReader{data: data}, "ogg", "voice")
	require.NoError(t, err)
	require.Len(t, stream.chunks, 1)
	assert.Equal(t, "ogg", stream.chunks[0].Format)
	assert.Equal(t, "voice", stream.chunks[0].GetOptions().GetPreset())
	assert.Equal(t, data, stream.chunks[0].Data)
}

func TestSendChunks_MultipleChunks(t *testing.T) {
	bigData := make([]byte, chunkSize+100)
	for i := range bigData {
		bigData[i] = byte(i % 256)
	}
	stream := &mockStream{}
	err := sendChunks(stream, &bytesReader{data: bigData}, "mp4", "lecture")
	require.NoError(t, err)
	assert.Len(t, stream.chunks, 2)
	assert.Equal(t, "mp4", stream.chunks[0].Format)
	assert.Equal(t, "lecture", stream.chunks[0].GetOptions().GetPreset())
	assert.Empty(t, stream.chunks[1].Format)
}

func TestSendChunks_EmptyReader(t *testing.T) {
	stream := &mockStream{}
	err := sendChunks(stream, &bytesReader{data: nil}, "ogg", "voice")
	require.NoError(t, err)
	assert.Empty(t, stream.chunks)
}

func TestSendChunks_ReadError(t *testing.T) {
	stream := &mockStream{}
	err := sendChunks(stream, &errReader{}, "ogg", "voice")
	assert.Error(t, err)
}

// bytesReader is a minimal io.Reader for testing.
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// errReader always returns an error on Read.
type errReader struct{}

func (r *errReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestIsUnavailable_NilError(t *testing.T) {
	assert.False(t, isUnavailable(nil))
}

func TestIsUnavailable_UnavailableCode(t *testing.T) {
	err := status.Error(codes.Unavailable, "service down")
	assert.True(t, isUnavailable(err))
}

func TestIsUnavailable_DeadlineExceededCode(t *testing.T) {
	err := status.Error(codes.DeadlineExceeded, "timeout")
	assert.True(t, isUnavailable(err))
}

func TestIsUnavailable_OkCode(t *testing.T) {
	err := status.Error(codes.OK, "fine")
	assert.False(t, isUnavailable(err))
}

func TestIsUnavailable_InternalCode(t *testing.T) {
	err := status.Error(codes.Internal, "boom")
	assert.False(t, isUnavailable(err))
}

func TestIsUnavailable_NonStatusError(t *testing.T) {
	assert.False(t, isUnavailable(context.DeadlineExceeded))
}

func TestErrUnavailable(t *testing.T) {
	err := errUnavailable("test_service")
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "test_service")
	assert.Contains(t, err.Error(), "unavailable")
}

func TestProtoToReminderInfo_Nil(t *testing.T) {
	assert.Nil(t, protoToReminderInfo(nil))
}

func TestExtractJSON_NoBraces(t *testing.T) {
	assert.Equal(t, "no json here", extractJSON("no json here"))
}

func TestExtractJSON_SimpleObject(t *testing.T) {
	input := `{"key": "value"}`
	assert.Equal(t, input, extractJSON(input))
}

func TestExtractJSON_WithSurroundingText(t *testing.T) {
	input := `Here is the result: {"intent": "note", "confidence": 0.9} done.`
	got := extractJSON(input)
	assert.Equal(t, `{"intent": "note", "confidence": 0.9}`, got)
}

func TestExtractJSON_MultipleObjects(t *testing.T) {
	input := `{"first": 1} some text {"second": 2}`
	got := extractJSON(input)
	assert.Equal(t, `{"first": 1} some text {"second": 2}`, got)
}

func TestExtractJSON_NoClosingBrace(t *testing.T) {
	input := `{"key": "value"`
	assert.Equal(t, input, extractJSON(input))
}

func TestExtractJSON_EmptyString(t *testing.T) {
	assert.Equal(t, "", extractJSON(""))
}

func TestThinkTagRegexp_StripsThinkBlock(t *testing.T) {
	input := `<think>reasoning here</think>actual answer`
	got := thinkTagRegexp.ReplaceAllString(input, "")
	assert.Equal(t, "actual answer", got)
}

func TestThinkTagRegexp_NoThinkBlock(t *testing.T) {
	input := `just text`
	got := thinkTagRegexp.ReplaceAllString(input, "")
	assert.Equal(t, "just text", got)
}

func TestThinkTagRegexp_MultilineThinkBlock(t *testing.T) {
	input := "<think>line1\nline2\nline3</think>result"
	got := thinkTagRegexp.ReplaceAllString(input, "")
	assert.Equal(t, "result", got)
}
