package english

import (
	"cmp"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	pb "notes-bot/proto/english/v1"
)

type directionProgress struct {
	Next   int64  `json:"next_opportunity,omitempty"`
	Due    int64  `json:"due,omitempty"` // Legacy, in rounds of All.
	Level  int32  `json:"level"`
	Lapses int32  `json:"lapses"`
	Grade  string `json:"grade,omitempty"`
}

type progress struct {
	English       *directionProgress `json:"en,omitempty"`
	Russian       *directionProgress `json:"ru,omitempty"`
	Opportunities int64              `json:"opportunities,omitempty"`
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
func (p progress) remaining(next int64) int64 { return max(0, next-p.Opportunities-1) }

func (p progress) summary(language string) *pb.DirectionProgress {
	side := p.side(language)
	return &pb.DirectionProgress{LastGrade: side.Grade, SkippedRoundsRemaining: int32(p.remaining(side.Next))}
}

func earlierPrompt(en, ru int64) string {
	if en < ru {
		return "en"
	}
	if ru < en {
		return "ru"
	}
	return "" // A tie uses the presentation token's parity.
}

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
	Seed           string                     `json:"seed"`
	Round          int64                      `json:"round"`
	Queue          []string                   `json:"queue"`
	Position       int                        `json:"position"`
	Prompts        map[string]string          `json:"prompts,omitempty"`
	Progress       map[string]progress        `json:"progress"`
	Dictionaries   map[string]dictionaryRound `json:"dictionaries,omitempty"`
	SharedSchedule bool                       `json:"shared_schedule,omitempty"`

	cardVisibility cardVisibility
	dictionaryID   string
}

type dictionaryRound struct {
	Seed     string                  `json:"seed"`
	Round    int64                   `json:"round"`
	Queue    []string                `json:"queue"`
	Position int                     `json:"position"`
	Prompts  map[string]string       `json:"prompts,omitempty"`
	Due      map[string]directionDue `json:"due,omitempty"` // Legacy, in rounds of this selection.
}

type directionDue struct {
	English int64 `json:"en,omitempty"`
	Russian int64 `json:"ru,omitempty"`
}

var easyIntervals = [...]struct{ min, max int32 }{{2, 3}, {4, 5}, {7, 9}, {14, 16}}

func (s *session) dictionaryView(id string) *session {
	stored := s.Dictionaries[id]
	return &session{
		Seed: stored.Seed, Round: stored.Round, Queue: stored.Queue, Position: stored.Position,
		Prompts: stored.Prompts, Progress: s.Progress, dictionaryID: id,
	}
}

func (s *session) saveDictionaryView(view *session) {
	if s.Dictionaries == nil {
		s.Dictionaries = make(map[string]dictionaryRound)
	}
	s.Dictionaries[view.dictionaryID] = dictionaryRound{
		Seed: view.Seed, Round: view.Round, Queue: view.Queue, Position: view.Position, Prompts: view.Prompts,
	}
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
	card := proto.CloneOf(cards[s.Queue[index]])
	p := s.Progress[card.Id]
	en, ru := p.side("en"), p.side("ru")
	card.Lapses = p.Lapses + en.Lapses + ru.Lapses
	token := englishHash(fmt.Sprintf("%s:%d:%d:%s:%s", s.Seed, s.Round, index, card.Id, card.Revision))
	// Equal due rounds keep the original presentation token, including packs
	// downloaded before the server upgrade.
	english := englishToken(token)
	if prompt := s.Prompts[card.Id]; prompt != "" && english != (prompt == "en") {
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
	var emptyRounds int64 = -1
	for id := range cards {
		p := s.Progress[id]
		wait := p.remaining(min(p.side("en").Next, p.side("ru").Next))
		if emptyRounds == -1 || wait < emptyRounds {
			emptyRounds = wait
		}
	}
	// If all cards are resting, jump directly to the next nonempty round.
	opportunities := emptyRounds + 1
	s.Round += opportunities
	s.Queue = s.Queue[:0]
	s.Position = 0
	s.Prompts = make(map[string]string)
	ranks := make(map[string]string, len(cards))
	for id := range cards {
		p := s.Progress[id]
		p.Opportunities += opportunities
		s.Progress[id] = p
		en, ru := p.side("en").Next, p.side("ru").Next
		if min(en, ru) <= p.Opportunities {
			s.Queue = append(s.Queue, id)
			ranks[id] = englishHash(fmt.Sprintf("%s:%d:%s", s.Seed, s.Round, id))
			if prompt := earlierPrompt(en, ru); prompt != "" {
				s.Prompts[id] = prompt
			}
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
		p.Level = min(int32(len(easyIntervals)), p.Level+1)
		interval := easyIntervals[p.Level-1]
		skipped = s.chooseSkippedRounds(id, action, interval.min, interval.max)
	}
	p.Next = card.Opportunities + 1 + int64(skipped)
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
