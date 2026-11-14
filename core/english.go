package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "notes-bot/proto/english/v1"
)

// EnglishServer has one local-filesystem writer, like Core notes. The journal
// contains only progress, never a second editable copy of learning material.
type EnglishServer struct {
	pb.UnimplementedEnglishServiceServer
	Vault string
	Now   func() time.Time
}

type englishReview struct {
	At    int64  `json:"at"`
	Grade string `json:"grade"`
	Token string `json:"token"`
}
type englishProgress struct {
	Next     int64           `json:"next"`
	Interval int64           `json:"interval"`
	Streak   int32           `json:"streak"`
	Lapses   int32           `json:"lapses"`
	History  []englishReview `json:"history"`
}
type englishJournal struct {
	Version int                         `json:"version"`
	Cards   map[string]*englishProgress `json:"cards"`
}

func (s *EnglishServer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func englishError(err error) error {
	if status.Code(err) != codes.Unknown {
		return err
	}
	// Do not leak note contents, filesystem paths or JSON into transport logs.
	return status.Error(codes.Internal, "English storage unavailable")
}
func (s *EnglishServer) path(rel string) (string, error) { return resolveVaultPath(s.Vault, rel) }
func (s *EnglishServer) read(rel string) ([]byte, error) {
	path, err := s.path(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, errors.New("invalid English file")
	}
	return os.ReadFile(path)
}
func (s *EnglishServer) material() ([]*pb.Card, error) {
	var all []*pb.Card
	for _, file := range [][2]string{{englishWords, "word"}, {englishPhrases, "phrase"}} {
		data, err := s.read(file[0])
		if err != nil {
			return nil, err
		}
		cards, err := parseEnglish(string(data), file[1])
		if err != nil {
			return nil, err
		}
		all = append(all, cards...)
	}
	return all, nil
}
func (s *EnglishServer) journal() (*englishJournal, error) {
	data, err := s.read(".notes-bot/english-progress.json")
	if err != nil {
		return nil, err
	}
	j := &englishJournal{}
	if len(data) == 0 {
		return &englishJournal{Version: 1, Cards: map[string]*englishProgress{}}, nil
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, j); err != nil {
			return nil, err
		}
		if j.Version != 1 || j.Cards == nil {
			return nil, errors.New("unsupported English journal")
		}
	}
	return j, nil
}
func applyEnglishProgress(c *pb.Card, p *englishProgress) {
	c.NextAt = 0
	c.IntervalSeconds = 0
	c.Streak = 0
	c.Lapses = 0
	count := 0
	if p != nil {
		c.NextAt = p.Next
		c.IntervalSeconds = p.Interval
		c.Streak = p.Streak
		c.Lapses = p.Lapses
		count = len(p.History)
	}
	c.Revision = englishHash(englishPayload(c) + fmt.Sprintf("\x00%d", count))
}

func (s *EnglishServer) ListCards(_ context.Context, req *pb.ListCardsRequest) (*pb.ListCardsResponse, error) {
	if req.Kind != "" && req.Kind != "word" && req.Kind != "phrase" {
		return nil, status.Error(codes.InvalidArgument, "invalid kind")
	}
	cards, err := s.material()
	if err != nil {
		return nil, englishError(err)
	}
	j, err := s.journal()
	if err != nil {
		return nil, englishError(err)
	}
	result := &pb.ListCardsResponse{}
	for _, c := range cards {
		if req.Kind == "" || c.Kind == req.Kind {
			applyEnglishProgress(c, j.Cards[c.Id])
			result.Cards = append(result.Cards, c)
		}
	}
	sort.SliceStable(result.Cards, func(i, k int) bool { return result.Cards[i].NextAt < result.Cards[k].NextAt })
	return result, nil
}

// SaveCard commits the canonical line and its idempotency marker in ONE atomic
// replacement. Cards without a journal row are immediately reviewable. There is
// no registration step to lose between the vault write and a database commit.
func (s *EnglishServer) SaveCard(ctx context.Context, req *pb.SaveCardRequest) (*pb.SaveCardResponse, error) {
	if req == nil || validateOperationID(req.OperationId) != nil || !validateEnglishCard(req.Card) {
		return nil, status.Error(codes.InvalidArgument, "invalid English card")
	}
	operationUnlock, err := lockFile(ctx, filepath.Join(s.Vault, "english-save-operations"))
	if err != nil {
		return nil, englishError(err)
	}
	defer operationUnlock()
	c := proto.Clone(req.Card).(*pb.Card)
	if err := validateDate(c.Date); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid date")
	}
	c.Id = englishHash("english:" + req.OperationId)
	rel := englishWords
	if c.Kind == "phrase" {
		rel = englishPhrases
	}
	path, err := s.path(rel)
	if err != nil {
		return nil, englishError(err)
	}
	if err = s.prepareDirectory(filepath.Dir(path), 0755); err != nil {
		return nil, englishError(err)
	}
	unlock, err := lockFile(ctx, path)
	if err != nil {
		return nil, englishError(err)
	}
	defer unlock()
	data, err := s.read(rel)
	if err != nil {
		return nil, englishError(err)
	}
	payloadHash := englishHash(englishPayload(c))
	// Search both files: an operation cannot be reused with another kind.
	for _, f := range []string{englishWords, englishPhrases} {
		prior, e := s.read(f)
		if e != nil {
			return nil, englishError(e)
		}
		for _, line := range strings.Split(string(prior), "\n") {
			m := englishMarker.FindStringSubmatch(strings.TrimSpace(line))
			if m == nil || m[1] != c.Id {
				continue
			}
			if f != rel || m[2] != payloadHash {
				return nil, status.Error(codes.AlreadyExists, "English save operation conflict")
			}
			// A previous write may have renamed successfully before its reply failed.
			handle, e := os.Open(path)
			if e != nil {
				return nil, englishError(e)
			}
			e = handle.Sync()
			handle.Close() //nolint:errcheck
			if e == nil {
				e = syncDirectory(filepath.Dir(path))
			}
			if e != nil {
				return nil, englishError(e)
			}
			cards, e := parseEnglish(string(prior), c.Kind)
			if e != nil {
				return nil, englishError(e)
			}
			for _, stored := range cards {
				if stored.Id == c.Id {
					journal, e := s.journal()
					if e != nil {
						return nil, englishError(e)
					}
					applyEnglishProgress(stored, journal.Cards[stored.Id])
					return &pb.SaveCardResponse{Card: stored}, nil
				}
			}
			return nil, status.Error(codes.FailedPrecondition, "saved card was edited; inspect English note")
		}
	}
	// Place the new entry inside its date section, preserving every existing byte.
	text := string(data)
	section := "[[" + c.Date + "]]"
	lines := strings.SplitAfter(text, "\n")
	start := -1
	offset := 0
	insert := len(text)
	for _, line := range lines {
		if m := englishDate.FindStringSubmatch(strings.TrimSuffix(line, "\n")); m != nil {
			if start >= 0 {
				insert = offset
				break
			}
			if m[1] == c.Date {
				start = offset
			}
		}
		offset += len(line)
	}
	prefix := ""
	if start < 0 {
		prefix = "\n" + section + "\n"
	}
	if insert > 0 && text[insert-1] != '\n' {
		prefix = "\n" + prefix
	}
	text = text[:insert] + prefix + renderEnglish(c, payloadHash) + text[insert:]
	perm := os.FileMode(0644)
	if info, e := os.Stat(path); e == nil {
		perm = info.Mode().Perm()
	}
	if err = writeFileAtomic(path, []byte(text), perm); err != nil {
		return nil, englishError(err)
	}
	journal, err := s.journal()
	if err != nil {
		return nil, englishError(err)
	}
	applyEnglishProgress(c, journal.Cards[c.Id])
	return &pb.SaveCardResponse{Card: c}, nil
}

// GradeCard is serialized across processes, compares material + history version,
// and persists every rating before returning. Retried last ratings are harmless.
func (s *EnglishServer) GradeCard(ctx context.Context, req *pb.GradeCardRequest) (*pb.GradeCardResponse, error) {
	if req.Grade != "miss" && req.Grade != "hard" && req.Grade != "easy" {
		return nil, status.Error(codes.InvalidArgument, "invalid grade")
	}
	path, err := s.path(".notes-bot/english-progress.json")
	if err != nil {
		return nil, englishError(err)
	}
	if err = s.prepareDirectory(filepath.Dir(path), 0700); err != nil {
		return nil, englishError(err)
	}
	unlock, err := lockFile(ctx, path)
	if err != nil {
		return nil, englishError(err)
	}
	defer unlock()
	cards, err := s.material()
	if err != nil {
		return nil, englishError(err)
	}
	var card *pb.Card
	for _, c := range cards {
		if c.Id == req.Id {
			card = c
			break
		}
	}
	if card == nil {
		return nil, status.Error(codes.NotFound, "card no longer exists")
	}
	j, err := s.journal()
	if err != nil {
		return nil, englishError(err)
	}
	p := j.Cards[card.Id]
	applyEnglishProgress(card, p)
	if p != nil && len(p.History) > 0 {
		last := p.History[len(p.History)-1]
		if last.Token == req.Revision && last.Grade == req.Grade {
			return &pb.GradeCardResponse{Card: card}, nil
		}
	}
	if card.Revision != req.Revision {
		return nil, status.Error(codes.FailedPrecondition, "card has changed")
	}
	if p == nil {
		p = &englishProgress{}
		j.Cards[card.Id] = p
	}
	scheduleEnglish(p, req.Grade, s.now())
	p.History = append(p.History, englishReview{At: s.now().Unix(), Grade: req.Grade, Token: req.Revision})
	data, err := json.Marshal(j)
	if err != nil {
		return nil, englishError(err)
	}
	if err = writeFileAtomic(path, data, 0600); err != nil {
		return nil, englishError(err)
	}
	applyEnglishProgress(card, p)
	return &pb.GradeCardResponse{Card: card}, nil
}
func scheduleEnglish(p *englishProgress, grade string, now time.Time) {
	const day = int64(24 * time.Hour / time.Second)
	switch grade {
	case "miss":
		p.Interval = 600
		p.Streak = 0
		p.Lapses++
	case "hard":
		p.Interval = min(180*day, max(day, p.Interval*5/4))
		p.Streak++
	case "easy":
		if p.Streak == 0 {
			p.Interval = day
		} else if p.Interval < 3*day {
			p.Interval = 3 * day
		} else {
			p.Interval = min(180*day, p.Interval*2)
		}
		p.Streak++
	}
	p.Next = now.Unix() + p.Interval
}

// Sync new directory entries as well as the file: a first save may create both
// Tags/English and the journal directory, unlike appending an existing note.
func (s *EnglishServer) prepareDirectory(dir string, perm os.FileMode) error {
	if err := os.MkdirAll(dir, perm); err != nil {
		return err
	}
	vault, err := filepath.Abs(s.Vault)
	if err != nil {
		return err
	}
	current, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for {
		if err := syncDirectory(current); err != nil {
			return err
		}
		if current == vault {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("english directory outside vault")
		}
		current = parent
	}
}
