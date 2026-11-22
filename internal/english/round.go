package english

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
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
	var next *pb.Card
	for _, id := range s.Queue[s.Position+1:] {
		if preview := cards[id]; preview != nil {
			next = preview
			break
		}
	}
	return &pb.ReviewPosition{
		NextPreview: next,
		Card:        card, Token: token, Round: s.Round,
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
		p.Level = 0
		skipped = s.chooseSkippedRounds(id, action, 0, 1)
	case "easy":
		intervals := [...]struct{ min, max int32 }{{2, 3}, {4, 5}, {7, 9}, {14, 16}}
		p.Level = min(int32(len(intervals)), p.Level+1)
		interval := intervals[p.Level-1]
		skipped = s.chooseSkippedRounds(id, action, interval.min, interval.max)
	}
	p.Due = s.Round + 1 + int64(skipped)
	s.Progress[id] = p
	return skipped
}

func (s *session) chooseSkippedRounds(id, action string, min, max int32) int32 {
	// Use the persisted seed and the review's identity so the choice is stable
	// across retries and independent of the card's position in the shuffle.
	input := fmt.Sprintf("review:%s:%d:%s:%s", s.Seed, s.Round, id, action)
	digest := sha256.Sum256([]byte(input))
	return min + int32(binary.BigEndian.Uint64(digest[:8])%uint64(max-min+1))
}
