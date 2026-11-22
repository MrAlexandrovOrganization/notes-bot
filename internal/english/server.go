package english

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/english/v1"
)

type Server struct {
	pb.UnimplementedEnglishServiceServer
	Dictionary Dictionary
	Store      Store
}

func rpcError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case status.Code(err) != codes.Unknown:
		return err
	default:
		return status.Error(codes.Internal, "English storage unavailable")
	}
}

func (s *Server) material(ctx context.Context) ([]*pb.Card, error) {
	snapshot, err := s.Dictionary.read(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.reviewCards()
}

func (snapshot dictionarySnapshot) reviewCards() ([]*pb.Card, error) {
	cards, err := snapshot.cards()
	for _, card := range cards {
		card.Revision = englishHash(englishPayload(card))
	}
	return cards, err
}

func (s *Server) ListCards(ctx context.Context, req *pb.ListCardsRequest) (*pb.ListCardsResponse, error) {
	kind := req.GetKind()
	if kind != "" && kind != "word" && kind != "phrase" {
		return nil, status.Error(codes.InvalidArgument, "invalid kind")
	}
	cards, err := s.material(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	resp := &pb.ListCardsResponse{}
	for _, card := range cards {
		if kind == "" || card.Kind == kind {
			resp.Cards = append(resp.Cards, card)
		}
	}
	return resp, nil
}

func (s *Server) SaveCard(ctx context.Context, req *pb.SaveCardRequest) (*pb.SaveCardResponse, error) {
	card, err := s.Dictionary.save(ctx, req)
	if err != nil {
		return nil, rpcError(err)
	}
	card.Revision = englishHash(englishPayload(card))
	return &pb.SaveCardResponse{Card: card}, nil
}

func (s *Server) GetReview(ctx context.Context, req *pb.GetReviewRequest) (*pb.GetReviewResponse, error) {
	resp := &pb.GetReviewResponse{}
	var remaining *[]*pb.ReviewPosition
	if req.GetIncludeRemaining() {
		remaining = &resp.Remaining
	}
	current, _, err := s.review(ctx, "", "", remaining)
	if err != nil {
		return nil, rpcError(err)
	}
	resp.Current = current
	return resp, nil
}

func (s *Server) AdvanceReview(ctx context.Context, req *pb.AdvanceReviewRequest) (*pb.AdvanceReviewResponse, error) {
	var action string
	switch req.GetAction() {
	case pb.ReviewAction_REVIEW_ACTION_MISS:
		action = "miss"
	case pb.ReviewAction_REVIEW_ACTION_HARD:
		action = "hard"
	case pb.ReviewAction_REVIEW_ACTION_EASY:
		action = "easy"
	case pb.ReviewAction_REVIEW_ACTION_SKIP:
		action = "skip"
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid review action")
	}
	if len(req.GetToken()) != 64 {
		return nil, status.Error(codes.InvalidArgument, "invalid review token")
	}
	current, skipped, err := s.review(ctx, req.GetToken(), action, nil)
	if err != nil {
		return nil, rpcError(err)
	}
	return &pb.AdvanceReviewResponse{Current: current, SkippedRounds: skipped}, nil
}

func (s *Server) review(ctx context.Context, token, action string, remaining *[]*pb.ReviewPosition) (*pb.ReviewPosition, int32, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var current *pb.ReviewPosition
	var skipped int32
	err := pgx.BeginFunc(ctx, s.Store.Pool, func(tx pgx.Tx) error {
		state, imported, err := loadSession(ctx, tx)
		if err != nil {
			return err
		}
		snapshot, err := s.Dictionary.read(ctx)
		if err != nil {
			return err
		}
		cards, err := snapshot.reviewCards()
		if err != nil {
			return err
		}
		byID := make(map[string]*pb.Card, len(cards))
		for _, card := range cards {
			byID[card.Id] = card
		}
		var changed bool
		current, changed = state.current(byID)
		if action != "" {
			var previous string
			err = tx.QueryRow(ctx, query("receipt"), token).Scan(&previous, &skipped)
			switch {
			case err == nil && previous != action:
				return status.Error(codes.FailedPrecondition, "review already advanced")
			case err == nil:
				// The caller may have lost the response after the transaction committed.
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			case current.GetToken() != token:
				return status.Error(codes.FailedPrecondition, "review presentation changed")
			default:
				id := current.Card.Id
				if action != "skip" {
					if err := s.Dictionary.pin(ctx, snapshot, id); err != nil {
						return err
					}
				}
				skipped = state.advance(id, action)
				if _, err := tx.Exec(ctx, query("record"), id, token, action, time.Now(), skipped, current.Round); err != nil {
					return err
				}
				current, _ = state.current(byID)
				changed = true
			}
		}
		if remaining != nil {
			*remaining = state.remaining(byID)
		}
		if changed {
			return saveSession(ctx, tx, state, imported)
		}
		return nil
	})
	return current, skipped, err
}
