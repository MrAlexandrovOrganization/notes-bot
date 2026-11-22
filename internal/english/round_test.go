package english

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/english/v1"
)

func TestEnglishRoundRatingLevelsAndCap(t *testing.T) {
	s := session{Seed: "fixed-test-seed", Round: 1, Progress: map[string]progress{}}
	for i, interval := range [][2]int32{{2, 3}, {4, 5}, {7, 9}, {14, 16}, {14, 16}} {
		skipped := s.advance("a", "easy")
		require.GreaterOrEqual(t, skipped, interval[0])
		require.LessOrEqual(t, skipped, interval[1])
		require.EqualValues(t, min(i+1, 4), s.Progress["a"].Level)
		require.Equal(t, s.Round+1+int64(skipped), s.Progress["a"].Due)
		s.Round = s.Progress["a"].Due
	}
	skipped := s.advance("a", "hard")
	require.Contains(t, []int32{0, 1}, skipped)
	require.Zero(t, s.Progress["a"].Level)
	require.Zero(t, s.Progress["a"].Lapses)
	require.Equal(t, s.Round+1+int64(skipped), s.Progress["a"].Due)
	s.Round = s.Progress["a"].Due
	require.Contains(t, []int32{2, 3}, s.advance("a", "easy"))
	s.Round = s.Progress["a"].Due
	require.Zero(t, s.advance("a", "miss"))
	require.Zero(t, s.Progress["a"].Level)
	require.EqualValues(t, 1, s.Progress["a"].Lapses)
	require.Equal(t, s.Round+1, s.Progress["a"].Due)
	s.Round = s.Progress["a"].Due
	require.Contains(t, []int32{2, 3}, s.advance("a", "easy"))

	// Progress saved before the interval change can still have level 5.
	s.Progress["old"] = progress{Level: 5, Lapses: 2}
	require.Contains(t, []int32{14, 15, 16}, s.advance("old", "easy"))
	require.EqualValues(t, 4, s.Progress["old"].Level)
	require.EqualValues(t, 2, s.Progress["old"].Lapses)

	before := s.Progress["a"]
	s.advance("a", "skip")
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
				skipped := first.advance(id, action)
				require.Equal(t, skipped, resumed.advance(id, action))
				intervals[skipped] = true
				laterSkipped := later.advance(id, action)
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
