package english

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/english/v1"
)

type cardVisibility struct {
	Hidden  map[string]bool `json:"hidden,omitempty"`
	Version int64           `json:"version,omitempty"`
}

func (s cardVisibility) response() *pb.CardVisibility {
	return &pb.CardVisibility{
		Version: s.Version, HiddenCardIds: slices.Sorted(maps.Keys(s.Hidden)),
	}
}

func (s *Server) SetCardHidden(ctx context.Context, req *pb.SetCardHiddenRequest) (*pb.SetCardHiddenResponse, error) {
	id, operation, hidden := req.GetCardId(), req.GetOperationId(), req.GetHidden()
	if !cardIdentity.MatchString(id) || !operationPattern.MatchString(operation) {
		return nil, status.Error(codes.InvalidArgument, "invalid English visibility operation")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := &pb.SetCardHiddenResponse{OperationId: operation}
	err := pgx.BeginFunc(ctx, s.Store.Pool, func(tx pgx.Tx) error {
		state, _, err := loadSession(ctx, tx)
		if err != nil {
			return err
		}
		var priorID string
		var priorHidden bool
		err = tx.QueryRow(ctx, query("read_visibility_operation"), operation).Scan(&priorID, &priorHidden)
		if err == nil {
			if priorID != id || priorHidden != hidden {
				return status.Error(codes.AlreadyExists, "English visibility operation conflict")
			}
			result.Visibility = state.cardVisibility.response()
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		snapshot, err := s.Dictionary.read(ctx)
		if err != nil {
			return err
		}
		cards, err := snapshot.cards()
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(cards, func(card *pb.Card) bool { return card.Id == id }) {
			return status.Error(codes.NotFound, "English card not found")
		}
		visibility := state.cardVisibility
		if visibility.Hidden[id] != hidden {
			if hidden {
				if err = s.Dictionary.pin(ctx, snapshot, id); err != nil {
					return err
				}
				if visibility.Hidden == nil {
					visibility.Hidden = make(map[string]bool)
				}
				visibility.Hidden[id] = true
			} else {
				delete(visibility.Hidden, id)
			}
			visibility.Version++
			data, err := json.Marshal(visibility)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, query("update_visibility"), data); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, query("record_visibility_operation"), operation, id, hidden); err != nil {
			return err
		}
		result.Visibility = visibility.response()
		return nil
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return result, nil
}
