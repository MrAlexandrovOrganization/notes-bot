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
	selection, err := snapshot.selectDictionary(req.GetDictionaryId())
	if err != nil {
		return nil, err
	}
	cards, err := snapshot.reviewCards()
	if err != nil {
		return nil, rpcError(err)
	}
	var state session
	if req.IncludeProgress || req.HiddenOnly {
		state, err = s.Store.readSession(ctx)
		if err != nil {
			return nil, rpcError(err)
		}
	}
	resp := &pb.ListCardsResponse{Dictionaries: snapshot.dictionaries(cards)}
	if req.IncludeProgress || req.HiddenOnly {
		resp.Visibility = state.cardVisibility.response()
	}
	for _, card := range cards {
		card.Hidden = state.cardVisibility.Hidden[card.Id]
		if !selection.includes(card.DictionaryId) || (req.HiddenOnly && !card.Hidden) {
			continue
		}
		if kind == "" || card.Kind == kind {
			if req.IncludeProgress {
				progress := state.Progress[card.Id]
				en, ru := progress.side("en"), progress.side("ru")
				if selection.key != allDictionaries {
					due := state.Dictionaries[selection.key].Due[card.Id]
					if _, started := state.Dictionaries[selection.key]; !started {
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
	result, err := s.review(ctx, req.GetDictionaryId(), "", "", pack)
	if err != nil {
		return nil, rpcError(err)
	}
	resp.Current = result.current
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
	result, err := s.review(ctx, req.GetDictionaryId(), req.GetToken(), action, nil)
	if err != nil {
		return nil, rpcError(err)
	}
	return &pb.AdvanceReviewResponse{Current: result.current, SkippedRounds: result.skipped, Visibility: result.visibility}, nil
}

type reviewResult struct {
	current    *pb.ReviewPosition
	skipped    int32
	visibility *pb.CardVisibility
}

func (s *Server) review(ctx context.Context, dictionary, token, action string, pack *pb.GetReviewResponse) (reviewResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result reviewResult
	err := pgx.BeginFunc(ctx, s.Store.Pool, func(tx pgx.Tx) error {
		state, imported, err := loadSession(ctx, tx)
		if err != nil {
			return err
		}
		snapshot, err := s.Dictionary.read(ctx)
		if err != nil {
			return err
		}
		selection, err := snapshot.selectDictionary(dictionary)
		if err != nil {
			return err
		}
		cards, err := snapshot.reviewCards()
		if err != nil {
			return err
		}
		byID := make(map[string]*pb.Card, len(cards))
		for _, card := range cards {
			if selection.includes(card.DictionaryId) && !state.cardVisibility.Hidden[card.Id] {
				byID[card.Id] = card
			}
		}
		view := &state
		var stored dictionaryRound
		scoped := selection.key != allDictionaries
		if scoped {
			view, stored = state.dictionaryView(selection.key, byID)
		}
		current, changed := view.current(byID)
		if action != "" {
			var previous string
			err = tx.QueryRow(ctx, query("receipt"), token).Scan(&previous, &result.skipped)
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
				result.skipped = view.advance(id, current.PromptLanguage, action)
				if scoped && action != "skip" {
					state.saveDictionaryGrade(stored, id, current.PromptLanguage, view.Progress[id].side(current.PromptLanguage))
				}
				if _, err := tx.Exec(ctx, query("record"), id, token, action, time.Now(), result.skipped, current.Round); err != nil {
					return err
				}
				current, _ = view.current(byID)
				changed = true
			}
		}
		result.current = current
		if action != "" || pack != nil {
			result.visibility = state.cardVisibility.response()
		}
		if pack != nil {
			pack.Remaining = view.remaining(byID)
			pack.Dictionaries = snapshot.dictionaries(cards)
			pack.Visibility = result.visibility
		}
		if changed {
			if scoped {
				state.saveDictionaryView(view, stored)
			}
			return saveSession(ctx, tx, state, imported)
		}
		return nil
	})
	return result, err
}
