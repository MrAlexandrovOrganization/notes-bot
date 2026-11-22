package english

import (
	"cmp"
	"crypto/rand"
	"fmt"
	"slices"

	pb "notes-bot/proto/english/v1"
)

type progress struct {
	Due    int64 `json:"due"`
	Level  int32 `json:"level"`
	Lapses int32 `json:"lapses"`
}

type session struct {
	Seed     string              `json:"seed"`
	Round    int64               `json:"round"`
	Queue    []string            `json:"queue"`
	Position int                 `json:"position"`
	Progress map[string]progress `json:"progress"`
}

func (s *session) current(cards map[string]*pb.Card) (*pb.ReviewPosition, bool) {
	changed := false
	for s.Position < len(s.Queue) && cards[s.Queue[s.Position]] == nil {
		s.Position++
		changed = true
	}
	if len(cards) == 0 {
		return &pb.ReviewPosition{}, changed
	}
	if s.Position == len(s.Queue) {
		s.nextRound(cards)
		changed = true
	}
	card := cards[s.Queue[s.Position]]
	p := s.Progress[card.Id]
	card.Lapses = p.Lapses
	token := englishHash(fmt.Sprintf("%s:%d:%d:%s:%s", s.Seed, s.Round, s.Position, card.Id, card.Revision))
	return &pb.ReviewPosition{
		Card: card, Token: token, Round: s.Round,
		Position: int32(s.Position + 1), RoundSize: int32(len(s.Queue)), EasyLevel: p.Level,
	}, changed
}

func (s *session) nextRound(cards map[string]*pb.Card) {
	if s.Seed == "" {
		s.Seed = rand.Text()
	}
	s.Round++
	earliest := int64(0)
	for id := range cards {
		due := max(s.Round, s.Progress[id].Due)
		if earliest == 0 || due < earliest {
			earliest = due
		}
	}
	// If all cards are resting, jump directly to the next nonempty round.
	s.Round = earliest
	s.Queue = s.Queue[:0]
	s.Position = 0
	ranks := make(map[string]string, len(cards))
	for id := range cards {
		if s.Progress[id].Due <= s.Round {
			s.Queue = append(s.Queue, id)
			ranks[id] = englishHash(fmt.Sprintf("%s:%d:%s", s.Seed, s.Round, id))
		}
	}
	slices.SortFunc(s.Queue, func(a, b string) int {
		if order := cmp.Compare(ranks[a], ranks[b]); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
}

func (s *session) advance(id, action string) int32 {
	s.Position++
	if action == "skip" {
		return 0
	}
	p := s.Progress[id]
	var skipped int32
	switch action {
	case "miss":
		p.Level = 0
		p.Lapses++
	case "hard":
		p.Level = max(0, p.Level-1)
	case "easy":
		p.Level = min(5, p.Level+1)
		skipped = 1 << (p.Level - 1)
	}
	p.Due = s.Round + 1 + int64(skipped)
	s.Progress[id] = p
	return skipped
}
