package english

import (
	"context"
	"encoding/json"
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
	snapshot, err := s.Dictionary.read(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	id, err := snapshot.selectDictionary(req.GetDictionaryId())
	if err != nil {
		return nil, err
	}
	cards, err := snapshot.reviewCards()
	if err != nil {
		return nil, rpcError(err)
	}
	var state session
	if req.IncludeProgress {
		var data []byte
		if err := s.Store.Pool.QueryRow(ctx, query("read_progress")).Scan(&data); err != nil {
			return nil, rpcError(err)
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, rpcError(err)
		}
	}
	resp := &pb.ListCardsResponse{Dictionaries: snapshot.dictionaries(cards)}
	for _, card := range cards {
		if id != allDictionaries && card.DictionaryId != id {
			continue
		}
		if kind == "" || card.Kind == kind {
			if req.IncludeProgress {
				progress := state.Progress[card.Id]
				en, ru := progress.side("en"), progress.side("ru")
				if id != allDictionaries {
					due := state.Dictionaries[id].Due[card.Id]
					if _, started := state.Dictionaries[id]; !started {
						due = directionDue{max(0, en.Due-state.Round), max(0, ru.Due-state.Round)}
					}
					en.Due, ru.Due = due.English, due.Russian
				}
				card.EnToRu, card.RuToEn = en.summary(), ru.summary()
			}
			resp.Cards = append(resp.Cards, card)
		}
	}
	return resp, nil
}

func (s *Server) ListDictionaries(ctx context.Context, _ *pb.ListDictionariesRequest) (*pb.ListDictionariesResponse, error) {
	snapshot, err := s.Dictionary.read(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	cards, err := snapshot.cards()
	if err != nil {
		return nil, rpcError(err)
	}
	return &pb.ListDictionariesResponse{Dictionaries: snapshot.dictionaries(cards)}, nil
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
	var pack *pb.GetReviewResponse
	if req.GetIncludeRemaining() {
		pack = resp
	}
	current, _, err := s.review(ctx, req.GetDictionaryId(), "", "", pack)
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
	current, skipped, err := s.review(ctx, req.GetDictionaryId(), req.GetToken(), action, nil)
	if err != nil {
		return nil, rpcError(err)
	}
	return &pb.AdvanceReviewResponse{Current: current, SkippedRounds: skipped}, nil
}

func (s *Server) review(ctx context.Context, dictionary, token, action string, pack *pb.GetReviewResponse) (*pb.ReviewPosition, int32, error) {
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
		dictionary, err := snapshot.selectDictionary(dictionary)
		if err != nil {
			return err
		}
		cards, err := snapshot.reviewCards()
		if err != nil {
			return err
		}
		byID := make(map[string]*pb.Card, len(cards))
		for _, card := range cards {
			if dictionary == allDictionaries || card.DictionaryId == dictionary {
				byID[card.Id] = card
			}
		}
		view := &state
		var stored dictionaryRound
		scoped := dictionary != allDictionaries
		if scoped {
			view, stored = state.dictionaryView(dictionary, byID)
		}
		var changed bool
		current, changed = view.current(byID)
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
				skipped = view.advance(id, current.PromptLanguage, action)
				if scoped && action != "skip" {
					state.saveDictionaryGrade(stored, id, current.PromptLanguage, view.Progress[id].side(current.PromptLanguage))
				}
				if _, err := tx.Exec(ctx, query("record"), id, token, action, time.Now(), skipped, current.Round); err != nil {
					return err
				}
				current, _ = view.current(byID)
				changed = true
			}
		}
		if pack != nil {
			pack.Remaining = view.remaining(byID)
			pack.Dictionaries = snapshot.dictionaries(cards)
		}
		if changed {
			if scoped {
				state.saveDictionaryView(view, stored)
			}
			return saveSession(ctx, tx, state, imported)
		}
		return nil
	})
	return current, skipped, err
}
