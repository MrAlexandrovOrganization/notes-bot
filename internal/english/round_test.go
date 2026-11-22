package english

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "notes-bot/proto/english/v1"
)

func TestEnglishRoundRatingLevelsAndCap(t *testing.T) {
	s := session{Round: 1, Progress: map[string]progress{}}
	for _, rounds := range []int32{1, 2, 4, 8, 16, 16} {
		require.Equal(t, rounds, s.advance("a", "easy"))
		require.Equal(t, s.Round+1+int64(rounds), s.Progress["a"].Due)
		s.Round = s.Progress["a"].Due
	}
	require.Zero(t, s.advance("a", "hard"))
	require.EqualValues(t, 4, s.Progress["a"].Level)
	require.Equal(t, s.Round+1, s.Progress["a"].Due)
	require.EqualValues(t, 16, s.advance("a", "easy"))
	require.Zero(t, s.advance("a", "miss"))
	require.EqualValues(t, 0, s.Progress["a"].Level)
	require.EqualValues(t, 1, s.Progress["a"].Lapses)
	require.Equal(t, s.Round+1, s.Progress["a"].Due)
	require.EqualValues(t, 1, s.advance("a", "easy"))
	before := s.Progress["a"]
	s.advance("a", "skip")
	require.Equal(t, before, s.Progress["a"])
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
