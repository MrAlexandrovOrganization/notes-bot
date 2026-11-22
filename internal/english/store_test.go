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
	seen := map[string]bool{start.Card.English: true}
	current := updated
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
