package tghandlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/english/v1"
)

func TestEnglishReviewResumesBackendPositionAndRetriesLostRating(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	client.review = &pb.ReviewPosition{
		Card:  &pb.Card{English: "yield", Source: "уступать дорогу"},
		Token: "backend", Round: 3, Position: 4, RoundSize: 7,
	}
	englishAction(t, app, bot, q, "review")
	require.Contains(t, api.texts[len(api.texts)-1], "Круг 3 · Карточка 4 из 7")
	require.NotContains(t, api.texts[len(api.texts)-1], "yield")
	nonce := state.value.English.Nonce
	englishAction(t, app, bot, q, "grade", nonce, "miss")
	require.Zero(t, client.grades)
	englishAction(t, app, bot, q, "hint", nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "y…")
	englishAction(t, app, bot, q, "show", nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "yield")
	client.loseGrade = true
	englishAction(t, app, bot, q, "grade", nonce, "miss")
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishGradeFailure)
	englishAction(t, app, bot, q, "grade", nonce, "miss")
	require.Equal(t, 1, client.grades)
	require.Equal(t, "next", state.value.English.Review.Token)
	require.Contains(t, api.texts[len(api.texts)-1], "Круг 4")
	englishAction(t, app, bot, q, "menu")
	state.value = tgstates.UserContext{}
	englishAction(t, app, bot, q, "review")
	require.Equal(t, "next", state.value.English.Review.Token)
	require.False(t, state.value.English.Revealed)
	require.Zero(t, llm.calls)
}

func TestEnglishReviewLegacyScreenCanResumeAndSkip(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	client.review = &pb.ReviewPosition{
		Card:  &pb.Card{English: "yield", Source: "уступать дорогу"},
		Token: "backend", Round: 1, Position: 1, RoundSize: 1,
	}
	state.value = tgstates.UserContext{State: tgstates.StateEnglishReview, English: tgstates.EnglishContext{Nonce: "old"}}
	englishAction(t, app, bot, q, "next", "old")
	require.Equal(t, "backend", state.value.English.Review.Token)
	englishAction(t, app, bot, q, "skip", state.value.English.Nonce)
	require.Equal(t, "next", state.value.English.Review.Token)
	require.Zero(t, client.grades)
	client.review = nil
	englishAction(t, app, bot, q, "review")
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishEmpty)
}
