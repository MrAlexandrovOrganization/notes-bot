package english

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/english/v1"
)

func TestEnglishRoundRatingLevelsAndCap(t *testing.T) {
	s := session{Seed: "fixed-test-seed", Round: 1, Progress: map[string]progress{}}
	for i, interval := range [][2]int32{{2, 3}, {4, 5}, {7, 9}, {14, 16}, {14, 16}} {
		skipped := s.advance("a", "ru", "easy")
		require.GreaterOrEqual(t, skipped, interval[0])
		require.LessOrEqual(t, skipped, interval[1])
		require.EqualValues(t, min(i+1, 4), s.Progress["a"].Russian.Level)
		require.Equal(t, s.Round+1+int64(skipped), s.Progress["a"].Russian.Due)
		s.Round = s.Progress["a"].Russian.Due
	}
	skipped := s.advance("a", "ru", "hard")
	require.Contains(t, []int32{0, 1}, skipped)
	require.Zero(t, s.Progress["a"].Russian.Level)
	require.Zero(t, s.Progress["a"].Russian.Lapses)
	require.Equal(t, s.Round+1+int64(skipped), s.Progress["a"].Russian.Due)
	s.Round = s.Progress["a"].Russian.Due
	require.Contains(t, []int32{2, 3}, s.advance("a", "ru", "easy"))
	s.Round = s.Progress["a"].Russian.Due
	require.Zero(t, s.advance("a", "ru", "miss"))
	require.Zero(t, s.Progress["a"].Russian.Level)
	require.EqualValues(t, 1, s.Progress["a"].Russian.Lapses)
	require.Equal(t, s.Round+1, s.Progress["a"].Russian.Due)
	s.Round = s.Progress["a"].Russian.Due
	require.Contains(t, []int32{2, 3}, s.advance("a", "ru", "easy"))

	// Progress saved before the interval change can still have level 5.
	s.Progress["old"] = progress{Russian: &directionProgress{Level: 5, Lapses: 2}}
	require.Contains(t, []int32{14, 15, 16}, s.advance("old", "ru", "easy"))
	require.EqualValues(t, 4, s.Progress["old"].Russian.Level)
	require.EqualValues(t, 2, s.Progress["old"].Russian.Lapses)

	before := s.Progress["a"]
	s.advance("a", "ru", "skip")
	require.Equal(t, before, s.Progress["a"])
}

func TestEnglishReviewOffsetsVaryByCardAndRound(t *testing.T) {
	for _, action := range []string{"hard", "easy"} {
		t.Run(action, func(t *testing.T) {
			first := session{Seed: "fixed-test-seed", Round: 5, Progress: map[string]progress{}}
			resumed := session{Seed: first.Seed, Round: first.Round, Progress: map[string]progress{}}
			later := session{Seed: first.Seed, Round: first.Round + 1, Progress: map[string]progress{}}
			intervals := map[int32]bool{}
			changed := false
			for card := range 32 {
				id := strconv.Itoa(card)
				skipped := first.advance(id, "ru", action)
				require.Equal(t, skipped, resumed.advance(id, "ru", action))
				intervals[skipped] = true
				laterSkipped := later.advance(id, "ru", action)
				changed = changed || laterSkipped != skipped
			}
			require.Len(t, intervals, 2)
			require.True(t, changed)
		})
	}
}

func TestEnglishRoundsDeterministicallyShuffleAndAdvancePastEmptyRounds(t *testing.T) {
	cards := map[string]*pb.Card{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		cards[id] = &pb.Card{Id: id}
	}
	first := session{Seed: "fixed-test-seed", Progress: map[string]progress{}}
	second := session{Seed: first.Seed, Progress: map[string]progress{}}
	first.nextRound(cards)
	second.nextRound(cards)
	require.Equal(t, first.Queue, second.Queue)
	require.Len(t, first.Queue, len(cards))
	order := append([]string(nil), first.Queue...)
	first.nextRound(cards)
	require.NotEqual(t, order, first.Queue)
	for id := range cards {
		first.Progress[id] = progress{Due: 19}
	}
	first.nextRound(cards)
	require.EqualValues(t, 19, first.Round)
	require.Len(t, first.Queue, len(cards))
}

func TestOfflineTokensMatchLivePresentations(t *testing.T) {
	cards := map[string]*pb.Card{"a": {Id: "a", Revision: "1"}, "b": {Id: "b", Revision: "2"}}
	s := session{Seed: "stable", Round: 7, Queue: []string{"a", "removed", "b"}, Progress: map[string]progress{}}
	before := s.remaining(cards)
	if len(before) != 2 || s.Position != 0 {
		t.Fatal("snapshot moved cursor")
	}
	for _, want := range before {
		got, _ := s.current(cards)
		if got.Token != want.Token || got.Position != want.Position {
			t.Fatal("offline token differs")
		}
		s.advance(got.Card.Id, got.PromptLanguage, "easy")
	}
}

func TestDirectionalGradesSurviveOfflineRoundAndRestart(t *testing.T) {
	cards := map[string]*pb.Card{"apple": {Id: "apple", Revision: "r1"}}
	s := session{Seed: "upgrade", Round: 10, Queue: []string{"apple"}, Progress: map[string]progress{"apple": {Due: 10, Level: 3, Lapses: 4}}}
	cached := s.remaining(cards)[0]
	require.Equal(t, englishHash("upgrade:10:0:apple:r1"), cached.Token)
	first := cached.PromptLanguage
	second := "en"
	if first == "en" {
		second = "ru"
	}
	skipped := s.advance("apple", first, "easy")
	require.Contains(t, []int32{2, 3}, skipped)
	due := s.Progress["apple"].side(first)
	for _, round := range []int64{11, 12} {
		pack, _ := s.current(cards)
		require.Equal(t, round, pack.Round)
		require.Equal(t, second, pack.PromptLanguage)
		require.Equal(t, second == "en", englishToken(pack.Token))
		require.EqualValues(t, 1, pack.RoundSize)
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var reloaded session
		require.NoError(t, json.Unmarshal(data, &reloaded))
		got, _ := reloaded.current(cards)
		require.Equal(t, pack.Token, got.Token)
		s = reloaded
		s.advance("apple", second, "miss")
	}
	require.Equal(t, due, s.Progress["apple"].side(first))
	require.Equal(t, "easy", due.Grade)
	require.Equal(t, "miss", s.Progress["apple"].side(second).Grade)
	require.EqualValues(t, 2, s.Progress["apple"].side(second).Lapses)
}
