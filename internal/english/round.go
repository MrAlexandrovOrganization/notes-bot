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

type directionProgress struct {
	Due    int64  `json:"due"`
	Level  int32  `json:"level"`
	Lapses int32  `json:"lapses"`
	Grade  string `json:"grade,omitempty"`
}

func (p directionProgress) summary() *pb.DirectionProgress {
	return &pb.DirectionProgress{LastGrade: p.Grade, DueRound: p.Due}
}

type progress struct {
	English *directionProgress `json:"en,omitempty"`
	Russian *directionProgress `json:"ru,omitempty"`
	// Unattributed progress from versions with a shared schedule.
	Due    int64 `json:"due"`
	Level  int32 `json:"level"`
	Lapses int32 `json:"lapses"`
}

func (p progress) side(language string) directionProgress {
	side := p.Russian
	if language == "en" {
		side = p.English
	}
	if side != nil {
		return *side
	}
	// Keep the old eligibility round without assigning a historical grade to either side.
	return directionProgress{Due: p.Due}
}
func (p progress) due() int64 { return min(p.side("en").Due, p.side("ru").Due) }

// Android 1.6.1 and 1.6.2 use Java String.hashCode parity. For ASCII tokens
// its low bit equals the XOR of each byte's low bit (31 is odd).
func englishToken(token string) bool {
	var parity byte
	for i := 0; i < len(token); i++ {
		parity ^= token[i]
	}
	return parity&1 != 0
}

type session struct {
	Seed         string                     `json:"seed"`
	Round        int64                      `json:"round"`
	Queue        []string                   `json:"queue"`
	Position     int                        `json:"position"`
	Progress     map[string]progress        `json:"progress"`
	Dictionaries map[string]dictionaryRound `json:"dictionaries,omitempty"`

	cardVisibility cardVisibility
	dictionaryID   string
}

type dictionaryRound struct {
	Seed     string                  `json:"seed"`
	Round    int64                   `json:"round"`
	Queue    []string                `json:"queue"`
	Position int                     `json:"position"`
	Due      map[string]directionDue `json:"due"`
}

type directionDue struct {
	English int64 `json:"en,omitempty"`
	Russian int64 `json:"ru,omitempty"`
}

// Grades and easy levels belong to the card. Each queue has its own round
// numbers, so a new queue starts with the remaining waits from All.
func (s *session) dictionaryView(id string, cards map[string]*pb.Card) (*session, dictionaryRound) {
	stored, initialized := s.Dictionaries[id]
	if !initialized {
		stored.Due = make(map[string]directionDue)
		for cardID := range cards {
			p := s.Progress[cardID]
			en, ru := p.side("en").Due, p.side("ru").Due
			if en > s.Round || ru > s.Round {
				stored.Due[cardID] = directionDue{max(0, en-s.Round), max(0, ru-s.Round)}
			}
		}
	}
	view := &session{
		Seed: stored.Seed, Round: stored.Round, Queue: stored.Queue, Position: stored.Position,
		Progress: make(map[string]progress, len(cards)), dictionaryID: id,
	}
	for cardID := range cards {
		p := s.Progress[cardID]
		en, ru := p.side("en"), p.side("ru")
		due := stored.Due[cardID]
		en.Due, ru.Due = due.English, due.Russian
		p.English, p.Russian = &en, &ru
		view.Progress[cardID] = p
	}
	return view, stored
}

func (s *session) saveDictionaryView(view *session, stored dictionaryRound) {
	stored.Seed, stored.Round, stored.Queue, stored.Position = view.Seed, view.Round, view.Queue, view.Position
	if s.Dictionaries == nil {
		s.Dictionaries = make(map[string]dictionaryRound)
	}
	s.Dictionaries[view.dictionaryID] = stored
}

func (s *session) saveDictionaryGrade(stored dictionaryRound, id, language string, graded directionProgress) {
	p := s.Progress[id]
	due := stored.Due[id]
	queueDue := graded.Due
	// Preserve the other queue's due rounds and presentation tokens.
	graded.Due = p.side(language).Due
	if language == "en" {
		due.English, p.English = queueDue, &graded
	} else {
		due.Russian, p.Russian = queueDue, &graded
	}
	stored.Due[id] = due
	s.Progress[id] = p
}

func (s *session) current(cards map[string]*pb.Card) (*pb.ReviewPosition, bool) {
	changed := false
	for s.Position < len(s.Queue) && cards[s.Queue[s.Position]] == nil {
		s.Position++
		changed = true
	}
	if len(cards) == 0 {
		return &pb.ReviewPosition{DictionaryId: s.dictionaryID}, changed
	}
	if s.Position == len(s.Queue) {
		s.nextRound(cards)
		changed = true
	}
	current := s.positionAt(cards, s.Position)
	for _, id := range s.Queue[s.Position+1:] {
		if preview := cards[id]; preview != nil {
			current.NextPreview = preview
			break
		}
	}
	return current, changed
}

func (s *session) positionAt(cards map[string]*pb.Card, index int) *pb.ReviewPosition {
	card := cards[s.Queue[index]]
	p := s.Progress[card.Id]
	en, ru := p.side("en"), p.side("ru")
	card.Lapses = p.Lapses + en.Lapses + ru.Lapses
	token := englishHash(fmt.Sprintf("%s:%d:%d:%s:%s", s.Seed, s.Round, index, card.Id, card.Revision))
	// Equal due rounds keep the original presentation token, including packs
	// downloaded before the server upgrade.
	english := englishToken(token)
	if en.Due != ru.Due && english != (en.Due < ru.Due) {
		english = !english
		// Preserve the legacy client's choice while keeping the token hexadecimal.
		if englishToken(token[:63]) == english {
			token = token[:63] + "0"
		} else {
			token = token[:63] + "1"
		}
	}
	language := "ru"
	if english {
		language = "en"
	}
	return &pb.ReviewPosition{Card: card, PromptLanguage: language, Token: token, Round: s.Round, Position: int32(index + 1), RoundSize: int32(len(s.Queue)), EasyLevel: p.side(language).Level, DictionaryId: s.dictionaryID}
}

func (s *session) remaining(cards map[string]*pb.Card) []*pb.ReviewPosition {
	var result []*pb.ReviewPosition
	for i := s.Position; i < len(s.Queue); i++ {
		if cards[s.Queue[i]] != nil {
			result = append(result, s.positionAt(cards, i))
		}
	}
	return result
}

func (s *session) nextRound(cards map[string]*pb.Card) {
	if s.Seed == "" {
		s.Seed = rand.Text()
	}
	s.Round++
	earliest := int64(0)
	for id := range cards {
		due := max(s.Round, s.Progress[id].due())
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
		if s.Progress[id].due() <= s.Round {
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

func (s *session) advance(id, language, action string) int32 {
	s.Position++
	if action == "skip" {
		return 0
	}
	card := s.Progress[id]
	p := card.side(language)
	p.Grade = action
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
	if language == "en" {
		card.English = &p
	} else {
		card.Russian = &p
	}
	s.Progress[id] = card
	return skipped
}

func (s *session) chooseSkippedRounds(id, action string, min, max int32) int32 {
	// Use the persisted seed and the review's identity so the choice is stable
	// across retries and independent of the card's position in the shuffle.
	input := fmt.Sprintf("review:%s:%d:%s:%s", s.Seed, s.Round, id, action)
	digest := sha256.Sum256([]byte(input))
	return min + int32(binary.BigEndian.Uint64(digest[:8])%uint64(max-min+1))
}
