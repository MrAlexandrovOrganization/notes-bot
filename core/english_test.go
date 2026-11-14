package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	pb "notes-bot/proto/english/v1"
)

func englishFixture(t *testing.T) *EnglishServer {
	t.Helper()
	return &EnglishServer{Vault: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }}
}
func englishWrite(t *testing.T, s *EnglishServer, rel, text string) {
	t.Helper()
	path := filepath.Join(s.Vault, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0644))
}
func englishRequest() *pb.SaveCardRequest {
	return &pb.SaveCardRequest{OperationId: "synthetic-save", Card: &pb.Card{Kind: "word", Date: "14-Nov-2026", English: "yield", Source: "уступать дорогу", Example: "Yield to pedestrians.", Context: "road sign"}}
}
func TestEnglishManualDatedListsRemainCanonical(t *testing.T) {
	s := englishFixture(t)
	words := "[[18-Sep-2024]]\n* `yield` - урожай\n\n[[10-Oct-2026]]\n- `radiance` - сияние\n"
	phrases := "[[10-Oct-2026]]\n- Я потратил полчаса, пытаясь это исправить → I spent half an hour trying to fix it.\n  Пояснение: spend time doing something\n"
	englishWrite(t, s, englishWords, words)
	englishWrite(t, s, englishPhrases, phrases)
	ctx := t.Context()
	first, err := s.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, first.Cards, 3)
	require.Equal(t, "18-Sep-2024", first.Cards[0].Date)
	require.Equal(t, "урожай", first.Cards[0].Source)
	again, err := s.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.True(t, proto.Equal(first, again))
	require.NoFileExists(t, filepath.Join(s.Vault, ".notes-bot/english-progress.json"))
	data, err := os.ReadFile(filepath.Join(s.Vault, englishWords))
	require.NoError(t, err)
	require.Equal(t, words, string(data))
	// Ordinary hand editing is discovered on the next read, no import or rewrite.
	englishWrite(t, s, englishWords, words+"- `layover` - пересадка\n")
	added, err := s.ListCards(ctx, &pb.ListCardsRequest{Kind: "word"})
	require.NoError(t, err)
	require.Len(t, added.Cards, 3)
	require.Equal(t, "10-Oct-2026", added.Cards[2].Date)
	englishWrite(t, s, englishWords, words+"- `layover` - пересадка\n- `layover` - пересадка\n")
	dedup, err := s.ListCards(ctx, &pb.ListCardsRequest{Kind: "word"})
	require.NoError(t, err)
	require.Len(t, dedup.Cards, 3)
	require.Equal(t, added.Cards[2].Id, dedup.Cards[2].Id)
}
func TestEnglishAtomicSaveRetryAndManualEdit(t *testing.T) {
	s := englishFixture(t)
	ctx := t.Context()
	req := englishRequest()
	prior := "[[18-Sep-2024]]\n* `yield` - урожай\n[[14-Nov-2026]]\n- `gate` - выход на посадку\n\n[[15-Nov-2026]]\n- `aisle` - проход\n"
	englishWrite(t, s, englishWords, prior)
	saved, err := s.SaveCard(ctx, req)
	require.NoError(t, err)
	// Crash after rename/before response: restart reads the same material marker.
	s = &EnglishServer{Vault: s.Vault, Now: s.Now}
	retry, err := s.SaveCard(ctx, req)
	require.NoError(t, err)
	require.Equal(t, saved.Card.Id, retry.Card.Id)
	path := filepath.Join(s.Vault, englishWords)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "Yield to pedestrians."))
	require.Less(t, strings.Index(string(data), "Yield to pedestrians."), strings.Index(string(data), "[[15-Nov-2026]]"))
	require.Contains(t, string(data), "* `yield` - урожай")
	require.Equal(t, 1, strings.Count(string(data), "[[14-Nov-2026]]"))
	changed := proto.Clone(req).(*pb.SaveCardRequest)
	changed.Card.Source = "производить"
	_, err = s.SaveCard(ctx, changed)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	changed.Card.Kind = "phrase"
	_, err = s.SaveCard(ctx, changed)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	// A bot marker is a stable identity, not a second material copy.
	englishWrite(t, s, englishWords, strings.Replace(string(data), "уступать дорогу", "пропускать пешеходов", 1))
	list, err := s.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	var edited *pb.Card
	for _, c := range list.Cards {
		if c.Id == saved.Card.Id {
			edited = c
		}
	}
	require.NotNil(t, edited)
	require.Equal(t, "пропускать пешеходов", edited.Source)
	require.NotEqual(t, saved.Card.Revision, edited.Revision)
	_, err = s.GradeCard(ctx, &pb.GradeCardRequest{Id: edited.Id, Revision: saved.Card.Revision, Grade: "easy"})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	_, err = s.SaveCard(ctx, req)
	require.NoError(t, err) // Never overwrite that manual edit.
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "пропускать пешеходов")
}
func TestEnglishReviewDurabilityIntervalsAndDuplicateCallbacks(t *testing.T) {
	s := englishFixture(t)
	ctx := t.Context()
	saved, err := s.SaveCard(ctx, englishRequest())
	require.NoError(t, err)
	card := saved.Card
	intervals := []int64{86400, 3 * 86400, 6 * 86400}
	var last *pb.GradeCardRequest
	for _, interval := range intervals {
		last = &pb.GradeCardRequest{Id: card.Id, Revision: card.Revision, Grade: "easy"}
		result, err := s.GradeCard(ctx, last)
		require.NoError(t, err)
		card = result.Card
		require.Equal(t, interval, card.IntervalSeconds)
		retry, err := s.GradeCard(ctx, last)
		require.NoError(t, err)
		require.Equal(t, card.Streak, retry.Card.Streak)
	}
	require.EqualValues(t, 3, card.Streak)
	_, err = s.GradeCard(ctx, &pb.GradeCardRequest{Id: card.Id, Revision: last.Revision, Grade: "hard"})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	missed, err := s.GradeCard(ctx, &pb.GradeCardRequest{Id: card.Id, Revision: card.Revision, Grade: "miss"})
	require.NoError(t, err)
	require.EqualValues(t, 0, missed.Card.Streak)
	require.EqualValues(t, 1, missed.Card.Lapses)
	require.EqualValues(t, 600, missed.Card.IntervalSeconds)
	restarted := &EnglishServer{Vault: s.Vault, Now: s.Now}
	list, err := restarted.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.True(t, proto.Equal(missed.Card, list.Cards[0]))
	hard, err := restarted.GradeCard(ctx, &pb.GradeCardRequest{Id: card.Id, Revision: missed.Card.Revision, Grade: "hard"})
	require.NoError(t, err)
	require.EqualValues(t, 86400, hard.Card.IntervalSeconds)
	journal, err := s.journal()
	require.NoError(t, err)
	require.Len(t, journal.Cards[card.Id].History, 5)
	// Corruption cannot silently reset a mature schedule.
	englishWrite(t, s, ".notes-bot/english-progress.json", "{}")
	_, err = s.ListCards(ctx, &pb.ListCardsRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	englishWrite(t, s, ".notes-bot/english-progress.json", "broken")
	_, err = s.ListCards(ctx, &pb.ListCardsRequest{})
	require.Equal(t, codes.Internal, status.Code(err))
	require.NotContains(t, err.Error(), s.Vault)
}
func TestEnglishConcurrentSaveAndGrade(t *testing.T) {
	s := englishFixture(t)
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() { _, err := s.SaveCard(ctx, englishRequest()); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	list, err := s.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, list.Cards, 1)
	card := list.Cards[0]
	errs = make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			_, err := s.GradeCard(ctx, &pb.GradeCardRequest{Id: card.Id, Revision: card.Revision, Grade: "easy"})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	journal, err := s.journal()
	require.NoError(t, err)
	require.Len(t, journal.Cards[card.Id].History, 1)
}
func TestEnglishFailureSafetyAndPhraseCreation(t *testing.T) {
	s := englishFixture(t)
	req := englishRequest()
	req.Card.Kind = "phrase"
	req.Card.English = "I spent half an hour trying to fix it."
	req.Card.Source = "Я потратил полчаса, пытаясь это исправить"
	_, err := s.SaveCard(t.Context(), req)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(s.Vault, englishPhrases))
	require.NoError(t, err)
	require.Contains(t, string(data), "[[14-Nov-2026]]\n- Я потратил")
	list, err := s.ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	require.Len(t, list.Cards, 1)
	require.NoError(t, os.Remove(filepath.Join(s.Vault, englishPhrases)))
	_, err = s.GradeCard(t.Context(), &pb.GradeCardRequest{Id: list.Cards[0].Id, Revision: list.Cards[0].Revision, Grade: "easy"})
	require.Equal(t, codes.NotFound, status.Code(err))
	req.Card.Source = "unsafe\nline"
	_, err = s.SaveCard(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	outside := t.TempDir()
	require.NoError(t, os.RemoveAll(filepath.Join(s.Vault, "Tags")))
	require.NoError(t, os.Symlink(outside, filepath.Join(s.Vault, "Tags")))
	req.Card.Source = "safe"
	_, err = s.SaveCard(t.Context(), req)
	require.Equal(t, codes.Internal, status.Code(err))
	require.NoFileExists(t, filepath.Join(outside, "English/phrases.md"))
}
func TestEnglishReadAndSkipDoNotGrade(t *testing.T) {
	s := englishFixture(t)
	_, err := s.SaveCard(t.Context(), englishRequest())
	require.NoError(t, err)
	for range 3 {
		_, err = s.ListCards(context.Background(), &pb.ListCardsRequest{})
		require.NoError(t, err)
	}
	require.NoFileExists(t, filepath.Join(s.Vault, ".notes-bot/english-progress.json"))
	p := &englishProgress{Interval: 100 * 86400, Streak: 10}
	scheduleEnglish(p, "hard", s.now())
	require.EqualValues(t, 125*86400, p.Interval)
	p.Interval = 180 * 86400
	scheduleEnglish(p, "easy", s.now())
	require.EqualValues(t, 180*86400, p.Interval)
}
