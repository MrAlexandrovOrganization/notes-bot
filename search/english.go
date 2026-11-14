package search

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"notes-bot/internal/englishtutor"
	pb "notes-bot/proto/english/v1"
)

// TutorServer uses only the chosen input. It deliberately has no pool, indexer,
// agent, retriever or vault dependency. Admission is bounded independently of RAG.
type TutorServer struct {
	pb.UnimplementedTutorServiceServer
	chat  *ChatClient
	slots chan struct{}
}

func NewTutorServer(chat *ChatClient) *TutorServer {
	return &TutorServer{chat: chat, slots: make(chan struct{}, 2)}
}
func (s *TutorServer) Prepare(ctx context.Context, req *pb.PrepareRequest) (*pb.PrepareResponse, error) {
	if req == nil || (req.Kind != "word" && req.Kind != "phrase") || strings.TrimSpace(req.Input) == "" || !utf8.ValidString(req.Input) || len([]rune(req.Input)) > 1000 || len([]rune(req.Instruction)) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "invalid learning input")
	}
	if s.chat == nil {
		return nil, status.Error(codes.Unavailable, "learning model disabled")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, status.Error(codes.ResourceExhausted, "learning model busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var previous *englishtutor.Draft
	if c := req.Previous; c != nil {
		previous = &englishtutor.Draft{Source: c.Source, English: c.English, Explanation: c.Explanation, Example: c.Example, Context: c.Context}
	}
	result, err := englishtutor.Prepare(ctx, req.Kind, req.Input, req.Instruction, previous, func(ctx context.Context, system, user string, schema any, tokens int) (string, error) {
		return s.chat.call(ctx, system, user, schema, tokens, 0)
	})
	if err != nil {
		return nil, status.Error(codes.Unavailable, "learning model unavailable")
	}
	return &pb.PrepareResponse{Card: &pb.Card{Kind: req.Kind, Source: result.Source, English: result.English, Explanation: result.Explanation, Example: result.Example, Context: result.Context}, Question: result.Question}, nil
}
