package tghandlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/english/v1"
)

func TestEnglishReviewResumesBackendPositionAndRetriesLostRating(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	client.review = &pb.ReviewPosition{
		Card:           &pb.Card{English: "yield", Source: "уступать дорогу"},
		PromptLanguage: "ru", Token: "backend", Round: 3, Position: 4, RoundSize: 7,
	}
	englishAction(t, app, bot, q, "review")
	require.Contains(t, api.markups[len(api.markups)-1], "english:dict:all")
	englishAction(t, app, bot, q, "dict", "all")
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
	englishAction(t, app, bot, q, "dict", "all")
	require.Equal(t, "next", state.value.English.Review.Token)
	require.False(t, state.value.English.Revealed)
	require.Zero(t, llm.calls)
}

type englishSelectionClient struct {
	*englishFlowClient
	scope  string
	hidden *pb.Card
}

func (*englishSelectionClient) ListDictionaries(context.Context, *pb.ListDictionariesRequest, ...grpc.CallOption) (*pb.ListDictionariesResponse, error) {
	return &pb.ListDictionariesResponse{Dictionaries: []*pb.DictionaryInfo{
		{Id: "all", Title: "Все слова"}, {Id: "general", Title: "Основной"}, {Id: "movie", Title: "Фильм"},
	}}, nil
}

func (c *englishSelectionClient) GetReview(_ context.Context, req *pb.GetReviewRequest, _ ...grpc.CallOption) (*pb.GetReviewResponse, error) {
	c.scope = req.DictionaryId
	c.review.DictionaryId = c.scope
	return &pb.GetReviewResponse{Current: c.review}, nil
}

func (c *englishSelectionClient) ListCards(_ context.Context, req *pb.ListCardsRequest, _ ...grpc.CallOption) (*pb.ListCardsResponse, error) {
	resp := &pb.ListCardsResponse{}
	if req.HiddenOnly && c.hidden != nil {
		resp.Cards = []*pb.Card{c.hidden}
	}
	return resp, nil
}

func (c *englishSelectionClient) SetCardHidden(_ context.Context, req *pb.SetCardHiddenRequest, _ ...grpc.CallOption) (*pb.SetCardHiddenResponse, error) {
	if req.Hidden && req.CardId == c.review.Card.Id {
		c.hidden = c.review.Card
		c.review = &pb.ReviewPosition{}
	} else if !req.Hidden && req.CardId == c.hidden.GetId() {
		c.hidden = nil
	}
	return &pb.SetCardHiddenResponse{}, nil
}

func TestEnglishMultiSelectionHideAndRestore(t *testing.T) {
	app, flow, _, api, bot, state, q := englishFlowFixture(t)
	client := &englishSelectionClient{englishFlowClient: flow}
	client.review = &pb.ReviewPosition{
		Card:  &pb.Card{Id: "card", English: "yield", Source: "уступать дорогу"},
		Token: "server", PromptLanguage: "en", Round: 1, Position: 1, RoundSize: 1,
	}
	app.English = client
	englishAction(t, app, bot, q, "review")
	require.Contains(t, api.markups[len(api.markups)-1], "english:multi")
	englishAction(t, app, bot, q, "multi")
	englishAction(t, app, bot, q, "pick", state.value.English.Nonce, "1")
	englishAction(t, app, bot, q, "pick", state.value.English.Nonce, "0")
	englishAction(t, app, bot, q, "use", state.value.English.Nonce)
	require.Equal(t, "movie+general", client.scope)
	englishAction(t, app, bot, q, "hide", state.value.English.Nonce)
	require.Equal(t, "movie+general", client.scope)
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishCardHidden)
	englishAction(t, app, bot, q, "hidden")
	require.Contains(t, api.markups[len(api.markups)-1], "yield")
	englishAction(t, app, bot, q, "restore", state.value.English.Nonce, "0")
	require.Nil(t, client.hidden)
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishNoHiddenCards)
}

func TestEnglishReviewLegacyScreenCanResumeAndSkip(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	client.review = &pb.ReviewPosition{
		Card:           &pb.Card{English: "yield", Source: "уступать дорогу"},
		PromptLanguage: "ru", Token: "backend", Round: 1, Position: 1, RoundSize: 1,
	}
	state.value = tgstates.UserContext{State: tgstates.StateEnglishReview, English: tgstates.EnglishContext{Nonce: "old"}}
	englishAction(t, app, bot, q, "next", "old")
	require.Equal(t, "backend", state.value.English.Review.Token)
	englishAction(t, app, bot, q, "skip", state.value.English.Nonce)
	require.Equal(t, "next", state.value.English.Review.Token)
	require.Zero(t, client.grades)
	client.review = nil
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "dict", "all")
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishEmpty)
}

func TestEnglishPromptHidesRussianUntilRevealed(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	client.review = &pb.ReviewPosition{Card: &pb.Card{English: "apple", Source: "яблоко"}, Token: "server", PromptLanguage: "en", Round: 1, Position: 1, RoundSize: 1}
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "dict", "all")
	front := api.texts[len(api.texts)-1]
	require.Contains(t, front, "apple")
	require.NotContains(t, front, "яблоко")
	nonce := state.value.English.Nonce
	englishAction(t, app, bot, q, "hint", nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "я…")
	englishAction(t, app, bot, q, "show", nonce)
	require.Contains(t, api.texts[len(api.texts)-1], "яблоко")
}
