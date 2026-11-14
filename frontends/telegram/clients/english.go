package clients

import (
	"context"
	"notes-bot/internal/englishtutor"
	pb "notes-bot/proto/english/v1"
)

func (c *CoreClient) English() pb.EnglishServiceClient { return pb.NewEnglishServiceClient(c.conn) }

type EnglishDraft = englishtutor.Draft
type EnglishGenerator = englishtutor.Generator

// PrepareEnglish shares the existing guarded search connection to the remote
// model worker. This RPC never performs retrieval or reads the personal vault.
func (c *SearchClient) PrepareEnglish(ctx context.Context, kind, input, instruction string, previous *EnglishDraft) (*EnglishDraft, error) {
	req := &pb.PrepareRequest{Kind: kind, Input: input, Instruction: instruction}
	if previous != nil {
		req.Previous = &pb.Card{Source: previous.Source, English: previous.English, Explanation: previous.Explanation, Example: previous.Example, Context: previous.Context}
	}
	result, err := pb.NewTutorServiceClient(c.conn).Prepare(ctx, req)
	if err != nil {
		return nil, err
	}
	card := result.Card
	return &EnglishDraft{Source: card.Source, English: card.English, Explanation: card.Explanation, Example: card.Example, Context: card.Context, Question: result.Question}, nil
}

func (c *LLMClient) PrepareEnglish(ctx context.Context, kind, input, instruction string, previous *EnglishDraft) (*EnglishDraft, error) {
	return englishtutor.Prepare(ctx, kind, input, instruction, previous, c.chat)
}
