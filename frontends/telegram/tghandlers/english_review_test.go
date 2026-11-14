package tghandlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/english/v1"
)

func writeEnglishReviewWords(t *testing.T, root string, count int) string {
	t.Helper()
	var words strings.Builder
	words.WriteString("[[14-Nov-2026]]\n")
	for i := range count {
		fmt.Fprintf(&words, "- `item%02d` - значение %d\n", i, i)
	}
	path := filepath.Join(root, "Tags/English/words.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(words.String()), 0644))
	return path
}

func TestEnglishReviewMissCannotStarveUnseenCards(t *testing.T) {
	app, client, llm, api, bot, state, q := englishFlowFixture(t)
	const count = 9
	writeEnglishReviewWords(t, client.server.Vault, count)
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "queue", "word")
	seen := map[string]bool{}
	for range count {
		e := state.value.English
		require.NotNil(t, e.Current)
		require.False(t, seen[e.Current.Id], "card repeated before all unseen cards were shown")
		seen[e.Current.Id] = true
		englishAction(t, app, bot, q, "show", e.Nonce)
		englishAction(t, app, bot, q, "grade", e.Nonce, "miss")
		englishAction(t, app, bot, q, "grade", e.Nonce, "miss") // Double clicks do not add a second missed entry.
		// Resume through JSON, as Redis and a restarted frontend do.
		raw, err := json.Marshal(state.value)
		require.NoError(t, err)
		state.value = tgstates.UserContext{}
		require.NoError(t, json.Unmarshal(raw, &state.value))
		englishAction(t, app, bot, q, "next", e.Nonce)
	}
	require.Len(t, seen, count)
	require.Nil(t, state.value.English.Current, "a pass must finish even when every card was forgotten")
	require.Equal(t, count, client.grades)
	require.Zero(t, llm.calls)
	require.Len(t, state.value.English.ReviewMissed, count)
	require.Len(t, state.value.English.ReviewSeen, count)
	require.Equal(t, count, state.value.English.ReviewTotal)
	require.Contains(t, api.markups[len(api.markups)-1], "english:missed:"+state.value.English.Nonce)
	cards, err := client.ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	for _, card := range cards.Cards {
		require.EqualValues(t, 1, card.Lapses)
		require.Greater(t, card.NextAt, time.Now().Unix())
	}
}

func TestEnglishReviewMissedRoundIsExplicitAndFinite(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	writeEnglishReviewWords(t, client.server.Vault, 5)
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "queue", "all")
	var missed []string
	for _, grade := range []string{"miss", "easy", "hard", "skip", "miss"} {
		e := state.value.English
		require.NotNil(t, e.Current)
		if grade == "skip" {
			englishAction(t, app, bot, q, "skip", e.Nonce)
			continue
		}
		if grade == "miss" {
			missed = append(missed, e.Current.Id)
		}
		englishAction(t, app, bot, q, "show", e.Nonce)
		englishAction(t, app, bot, q, "grade", e.Nonce, grade)
		englishAction(t, app, bot, q, "next", e.Nonce)
	}
	require.Nil(t, state.value.English.Current)
	require.Equal(t, missed, state.value.English.ReviewMissed)
	require.Equal(t, 4, client.grades) // The skipped card has no rating.
	endNonce := state.value.English.Nonce
	englishAction(t, app, bot, q, "missed", endNonce)
	require.Equal(t, 2, state.value.English.ReviewTotal)
	require.Empty(t, state.value.English.ReviewMissed)
	firstNonce := state.value.English.Nonce
	textBefore := len(api.texts)
	englishAction(t, app, bot, q, "missed", endNonce) // A stale double click must preserve the active card UI.
	require.Equal(t, firstNonce, state.value.English.Nonce)
	require.Len(t, api.texts, textBefore)
	for _, id := range missed {
		e := state.value.English
		require.Equal(t, id, e.Current.Id)
		englishAction(t, app, bot, q, "show", e.Nonce)
		englishAction(t, app, bot, q, "grade", e.Nonce, "miss")
		englishAction(t, app, bot, q, "next", e.Nonce)
	}
	require.Nil(t, state.value.English.Current)
	require.Equal(t, missed, state.value.English.ReviewMissed)
	require.Len(t, state.value.English.ReviewSeen, 2)
	require.Equal(t, 6, client.grades)
	englishAction(t, app, bot, q, "menu")
	require.Equal(t, tgstates.StateEnglishMenu, state.value.State)
	require.Equal(t, 6, client.grades)
}

func TestEnglishReviewSkipPassDoesNotChangeProgress(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	writeEnglishReviewWords(t, client.server.Vault, 3)
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "queue", "word")
	for range 3 {
		englishAction(t, app, bot, q, "skip", state.value.English.Nonce)
	}
	require.Nil(t, state.value.English.Current)
	require.Len(t, state.value.English.ReviewSeen, 3)
	require.Empty(t, state.value.English.ReviewMissed)
	require.Zero(t, client.grades)
	require.Contains(t, api.texts[len(api.texts)-1], r.EnglishPassFinished(3, 0))
	require.NotContains(t, api.markups[len(api.markups)-1], "english:missed:")
	require.NoFileExists(t, filepath.Join(client.server.Vault, ".notes-bot/english-progress.json"))
}

func TestEnglishReviewOldQueueContinuesWithoutReinsertedCurrentCard(t *testing.T) {
	app, client, _, _, bot, state, q := englishFlowFixture(t)
	writeEnglishReviewWords(t, client.server.Vault, 5)
	cards, err := client.ListCards(t.Context(), &pb.ListCardsRequest{})
	require.NoError(t, err)
	first := cards.Cards[0]
	graded, err := client.GradeCard(t.Context(), &pb.GradeCardRequest{Id: first.Id, Revision: first.Revision, Grade: "miss"})
	require.NoError(t, err)
	queue := []string{cards.Cards[1].Id, cards.Cards[2].Id, cards.Cards[3].Id, first.Id, cards.Cards[4].Id}
	// A real old session has no review_* fields and queues a forgotten current
	// card after three other cards. Keep its grade and finish the rest once.
	state.value.State = tgstates.StateEnglishReview
	state.value.English = tgstates.EnglishContext{Current: graded.Card, Graded: true, Revealed: true, Nonce: "legacy", Queue: queue}
	englishAction(t, app, bot, q, "next", "legacy")
	seen := []string{first.Id}
	for range 4 {
		e := state.value.English
		require.NotNil(t, e.Current)
		require.NotContains(t, seen, e.Current.Id)
		seen = append(seen, e.Current.Id)
		englishAction(t, app, bot, q, "skip", e.Nonce)
	}
	require.Nil(t, state.value.English.Current)
	require.Len(t, state.value.English.ReviewSeen, 5)
	require.Equal(t, 5, state.value.English.ReviewTotal)
	require.Equal(t, []string{first.Id}, state.value.English.ReviewMissed)
	require.Equal(t, 1, client.grades)
}

func TestEnglishReviewManualDeletionRemovesPendingAndMissedCards(t *testing.T) {
	app, client, _, api, bot, state, q := englishFlowFixture(t)
	path := writeEnglishReviewWords(t, client.server.Vault, 4)
	englishAction(t, app, bot, q, "review")
	englishAction(t, app, bot, q, "queue", "word")
	e := state.value.English
	englishAction(t, app, bot, q, "show", e.Nonce)
	englishAction(t, app, bot, q, "grade", e.Nonce, "miss")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := strings.ReplaceAll(string(data), "- `item00` - значение 0\n", "")
	text = strings.ReplaceAll(text, "- `item01` - значение 1\n", "")
	require.NoError(t, os.WriteFile(path, []byte(text), 0644))
	englishAction(t, app, bot, q, "next", e.Nonce)
	for range 2 {
		require.NotNil(t, state.value.English.Current)
		englishAction(t, app, bot, q, "skip", state.value.English.Nonce)
	}
	require.Nil(t, state.value.English.Current)
	require.Equal(t, 3, state.value.English.ReviewTotal) // One card was seen before deletion, two afterwards.
	require.Len(t, state.value.English.ReviewSeen, 3)
	require.Empty(t, state.value.English.ReviewMissed)
	require.NotContains(t, api.markups[len(api.markups)-1], "english:missed:")
	require.Equal(t, 1, client.grades)
}
