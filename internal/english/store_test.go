package english

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "notes-bot/proto/english/v1"
)

func testEnglishStore(t *testing.T) Store {
	t.Helper()
	dsn := os.Getenv("TEST_ENGLISH_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_ENGLISH_DATABASE_URL is unset")
	}
	ctx := t.Context()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	admin, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	_, err = admin.Exec(ctx, "CREATE SCHEMA english_integration")
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = "english_integration"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Close()
		_, err := admin.Exec(context.Background(), "DROP SCHEMA english_integration CASCADE")
		require.NoError(t, err)
	})
	store := Store{Pool: pool}
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))
	require.Error(t, store.Check(ctx))
	return store
}

func TestEnglishPostgresResumeMigrationAndConcurrentGrades(t *testing.T) {
	ctx := t.Context()
	store := testEnglishStore(t)
	files := &testMarkdown{files: map[string]string{englishWords: "- `one` - один\n- `two` - два\n- `three` - три\n"}}
	dictionary := Dictionary{Notes: files}
	cards, err := (&Server{Dictionary: dictionary}).ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	journal := filepath.Join(t.TempDir(), "legacy.json")
	require.NoError(t, os.WriteFile(journal, []byte(`{"version":1,"cards":{"`+cards.Cards[0].Id+`":{"lapses":1,"history":[{"at":123,"grade":"miss","token":"legacy-token"}]}}}`), 0600))
	require.NoError(t, store.ImportLegacy(ctx, journal))
	require.NoError(t, store.ImportLegacy(ctx, journal))
	require.NoError(t, store.Check(ctx))
	first := &Server{Dictionary: dictionary, Store: store}
	second := &Server{Dictionary: dictionary, Store: store}
	get := func(server *Server) *pb.ReviewPosition {
		response, err := server.GetReview(ctx, &pb.GetReviewRequest{})
		require.NoError(t, err)
		return response.Current
	}
	start := get(first)
	require.True(t, proto.Equal(start, get(second)))
	req := &pb.AdvanceReviewRequest{Token: start.Token, Action: pb.ReviewAction_REVIEW_ACTION_MISS}
	errs := make(chan error, 2)
	for _, server := range []*Server{first, second} {
		go func() {
			_, err := server.AdvanceReview(ctx, req)
			errs <- err
		}()
	}
	for range 2 {
		require.NoError(t, <-errs)
	}
	next := get(second)
	require.EqualValues(t, 2, next.Position)
	require.NotEqual(t, start.Card.Id, next.Card.Id)
	var history int
	require.NoError(t, store.Pool.QueryRow(ctx, "SELECT count(*) FROM english_review_history").Scan(&history))
	require.Equal(t, 2, history)
	req.Action = pb.ReviewAction_REVIEW_ACTION_HARD
	_, err = first.AdvanceReview(ctx, req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	// An edited prompt invalidates the token. A graded manual entry now has a
	// stable Markdown identity, and its progress survives the edit.
	require.Contains(t, files.files[englishWords], start.Card.Id)
	files.files[englishWords] = markHandwritten(files.files[englishWords], "word", next.Card.Id)
	files.files[englishWords] = strings.Replace(files.files[englishWords], next.Card.Source, "новое значение", 1)
	_, err = second.AdvanceReview(ctx, &pb.AdvanceReviewRequest{Token: next.Token, Action: pb.ReviewAction_REVIEW_ACTION_SKIP})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	updated := get(second)
	require.Equal(t, "новое значение", updated.Card.Source)
	require.NotEqual(t, next.Token, updated.Token)
	easy := &pb.AdvanceReviewRequest{Token: updated.Token, Action: pb.ReviewAction_REVIEW_ACTION_EASY}
	rated, err := first.AdvanceReview(ctx, easy)
	require.NoError(t, err)
	require.Contains(t, []int32{2, 3}, rated.SkippedRounds)
	list, err := first.ListCards(ctx, &pb.ListCardsRequest{Kind: "word", IncludeProgress: true})
	require.NoError(t, err)
	for _, c := range list.Cards {
		if c.Id != updated.Card.Id {
			continue
		}
		seenSide, otherSide := c.RuToEn, c.EnToRu
		if updated.PromptLanguage == "en" {
			seenSide, otherSide = otherSide, seenSide
		}
		require.Equal(t, "easy", seenSide.LastGrade)
		require.Equal(t, rated.SkippedRounds, seenSide.SkippedRoundsRemaining)
		require.Empty(t, otherSide.LastGrade)
	}
	replayed, err := second.AdvanceReview(ctx, easy)
	require.NoError(t, err)
	require.True(t, proto.Equal(rated, replayed))
	seen := map[string]bool{start.Card.English: true, updated.Card.English: true}
	current := rated.Current
	for current.Round == start.Round {
		require.False(t, seen[current.Card.English])
		seen[current.Card.English] = true
		result, err := first.AdvanceReview(ctx, &pb.AdvanceReviewRequest{Token: current.Token, Action: pb.ReviewAction_REVIEW_ACTION_HARD})
		require.NoError(t, err)
		current = result.Current
	}
	require.Len(t, seen, 3)
	require.EqualValues(t, 2, current.Round)
	require.True(t, proto.Equal(current, get(second)))
}

func TestEnglishPostgresDictionaryQueuesShareScheduleAndKeepTheirPositions(t *testing.T) {
	ctx := t.Context()
	store := testEnglishStore(t)
	require.NoError(t, store.ImportLegacy(ctx, filepath.Join(t.TempDir(), "missing.json")))
	files := &testMarkdown{files: map[string]string{englishWords: "- `one` - один\n- `two` - два\n- `three` - три\n- `four` - четыре\n"}}
	bot := &Server{Dictionary: Dictionary{Notes: files}, Store: store}
	android := &Server{Dictionary: bot.Dictionary, Store: store}
	list, err := bot.ListCards(ctx, &pb.ListCardsRequest{})
	require.NoError(t, err)
	initial, err := bot.GetReview(ctx, &pb.GetReviewRequest{IncludeRemaining: true})
	require.NoError(t, err)
	require.Len(t, initial.Remaining, 4)

	move := DictionaryMove{ID: "movie", Title: "Фильм", CardIDs: []string{list.Cards[0].Id, list.Cards[1].Id}}
	require.NoError(t, bot.MoveDictionary(ctx, move))
	movie, err := bot.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: move.ID, IncludeRemaining: true})
	require.NoError(t, err)
	require.Len(t, movie.Remaining, 2)
	for _, p := range movie.Remaining {
		require.Equal(t, move.ID, p.DictionaryId)
		require.Contains(t, move.CardIDs, p.Card.Id)
	}
	main, err := android.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: generalDictionary, IncludeRemaining: true})
	require.NoError(t, err)
	require.Len(t, main.Remaining, 2)
	require.NotContains(t, move.CardIDs, main.Current.Card.Id)
	req := &pb.AdvanceReviewRequest{Token: movie.Current.Token, DictionaryId: move.ID, Action: pb.ReviewAction_REVIEW_ACTION_EASY}
	scored, err := android.AdvanceReview(ctx, req)
	require.NoError(t, err)
	require.Contains(t, []int32{2, 3}, scored.SkippedRounds)
	resumed, err := bot.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: move.ID})
	require.NoError(t, err)
	require.True(t, proto.Equal(scored.Current, resumed.Current))
	retry, err := bot.AdvanceReview(ctx, req)
	require.NoError(t, err)
	require.True(t, proto.Equal(scored, retry))
	all, err := android.GetReview(ctx, &pb.GetReviewRequest{})
	require.NoError(t, err)
	require.Equal(t, initial.Current.Token, all.Current.Token)
	global, err := android.ListCards(ctx, &pb.ListCardsRequest{IncludeProgress: true})
	require.NoError(t, err)
	for _, card := range global.Cards {
		if movie.Current.Card.Id != card.Id {
			continue
		}
		graded := card.RuToEn
		if movie.Current.PromptLanguage == "en" {
			graded = card.EnToRu
		}
		require.Equal(t, "easy", graded.LastGrade)
		require.Equal(t, scored.SkippedRounds, graded.SkippedRoundsRemaining)
	}
	movieCards, err := bot.ListCards(ctx, &pb.ListCardsRequest{DictionaryId: move.ID, IncludeProgress: true})
	require.NoError(t, err)
	for _, card := range movieCards.Cards {
		if movie.Current.Card.Id != card.Id {
			continue
		}
		graded := card.RuToEn
		if movie.Current.PromptLanguage == "en" {
			graded = card.EnToRu
		}
		require.Equal(t, scored.SkippedRounds, graded.SkippedRoundsRemaining)
	}
}

func TestEnglishPostgresCombinedRoundsAndVisibility(t *testing.T) {
	ctx := t.Context()
	store := testEnglishStore(t)
	require.NoError(t, store.ImportLegacy(ctx, filepath.Join(t.TempDir(), "missing.json")))
	root := &testMarkdown{files: map[string]string{
		englishWords:                   "- `usual` - обычный\n",
		dictionariesPath + "/movie.md": "- `one` - один\n- `two` - два\n",
		dictionariesPath + "/extra.md": "- `three` - три\n",
	}}
	service := &Server{Dictionary: Dictionary{Notes: root}, Store: store}
	read := func(key string) *pb.GetReviewResponse {
		t.Helper()
		pack, err := service.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: key, IncludeRemaining: true})
		require.NoError(t, err)
		return pack
	}
	all := read("")
	single := read("movie")
	pack := read("movie+extra+movie")
	require.Len(t, pack.Remaining, 3)
	for _, p := range pack.Remaining {
		require.Equal(t, "extra+movie", p.DictionaryId)
		require.NotEqual(t, "usual", p.Card.English)
	}
	require.Equal(t, pack.Current.Token, read("extra+movie").Current.Token)
	require.Equal(t, single.Current.Token, read("movie").Current.Token)
	require.Equal(t, all.Current.Token, read("").Current.Token)

	graded := pack.Current
	next, err := service.AdvanceReview(ctx, &pb.AdvanceReviewRequest{
		DictionaryId: "movie+extra", Token: graded.Token, Action: pb.ReviewAction_REVIEW_ACTION_EASY,
	})
	require.NoError(t, err)
	require.NotEqual(t, graded.Card.Id, next.Current.Card.Id)
	hide := &pb.SetCardHiddenRequest{CardId: graded.Card.Id, Hidden: true, OperationId: "hide-first"}
	_, err = service.SetCardHidden(ctx, hide)
	require.NoError(t, err)
	second := &pb.SetCardHiddenRequest{CardId: next.Current.Card.Id, Hidden: true, OperationId: "hide-current"}
	_, err = service.SetCardHidden(ctx, second)
	require.NoError(t, err)

	reopened := read("extra+movie")
	require.Len(t, reopened.Remaining, 1)
	require.NotContains(t, reopened.Visibility.HiddenCardIds, reopened.Current.Card.Id)
	require.Len(t, reopened.Visibility.HiddenCardIds, 2)
	require.Len(t, read("general").Remaining, 1)
	_, err = service.AdvanceReview(ctx, &pb.AdvanceReviewRequest{
		DictionaryId: "extra+movie", Token: next.Current.Token, Action: pb.ReviewAction_REVIEW_ACTION_EASY,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	hidden, err := service.ListCards(ctx, &pb.ListCardsRequest{HiddenOnly: true, IncludeProgress: true})
	require.NoError(t, err)
	require.Len(t, hidden.Cards, 2)
	for _, card := range hidden.Cards {
		require.True(t, card.Hidden)
		if card.Id == graded.Card.Id {
			side := card.RuToEn
			if graded.PromptLanguage == "en" {
				side = card.EnToRu
			}
			require.Equal(t, "easy", side.LastGrade)
		}
	}

	restored, err := service.SetCardHidden(ctx, &pb.SetCardHiddenRequest{CardId: graded.Card.Id, OperationId: "restore-first"})
	require.NoError(t, err)
	require.EqualValues(t, 3, restored.Visibility.Version)
	retry, err := service.SetCardHidden(ctx, hide)
	require.NoError(t, err)
	require.True(t, proto.Equal(restored.Visibility, retry.Visibility))
	require.Equal(t, []string{second.CardId}, retry.Visibility.HiddenCardIds)
	hide.Hidden = false
	_, err = service.SetCardHidden(ctx, hide)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}
