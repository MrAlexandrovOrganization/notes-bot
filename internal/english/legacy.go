package english

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// ImportLegacy preserves historical grades and lapses. Round eligibility starts
// fresh because the previous schedule used elapsed time.
func (s Store) ImportLegacy(ctx context.Context, path string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		state, imported, err := loadSession(ctx, tx)
		if err != nil || imported {
			return err
		}
		journal, err := readLegacy(path)
		if err != nil {
			return err
		}
		for id, p := range journal {
			state.Progress[id] = progress{Lapses: p.Lapses}
			for _, review := range p.History {
				if _, err := tx.Exec(ctx, query("record"), id, review.Token, review.Grade, time.Unix(review.At, 0), 0, 0); err != nil {
					return err
				}
			}
		}
		return saveSession(ctx, tx, state, true)
	})
}

type legacyProgress struct {
	Lapses  int32
	History []struct {
		At    int64
		Grade string
		Token string
	}
}

func readLegacy(path string) (map[string]legacyProgress, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var journal struct {
		Version int
		Cards   map[string]legacyProgress
	}
	if len(data) > limit || json.Unmarshal(data, &journal) != nil || journal.Version != 1 || journal.Cards == nil {
		return nil, errors.New("invalid legacy English journal")
	}
	return journal.Cards, nil
}
