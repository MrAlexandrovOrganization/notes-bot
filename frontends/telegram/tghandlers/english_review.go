package tghandlers

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mymmrac/telego"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/timeutil"
	pb "notes-bot/proto/english/v1"
)

func (a *App) englishReviewMenu(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64) error {
	cards, err := a.English.ListCards(ctx, &pb.ListCardsRequest{})
	if err != nil {
		return err
	}
	words, phrases, forgotten := 0, 0, 0
	for _, c := range cards.Cards {
		if c.Lapses >= 2 {
			forgotten++
		}
		if c.NextAt <= time.Now().Unix() {
			if c.Kind == "word" {
				words++
			} else {
				phrases++
			}
		}
	}
	if err = a.setEnglish(ctx, user, tgstates.StateEnglishMenu, tgstates.EnglishContext{}); err != nil {
		return err
	}
	return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishCounts(words, phrases, len(cards.Cards), forgotten)), tgkeyboards.EnglishReviewKinds())
}

// Review snapshots hold only queue IDs. A card is shown once per pass, even if
// every answer is forgotten; missed cards form a separate, explicitly started pass.
func englishUniqueIDs(ids, excluded []string) []string {
	seen := make(map[string]bool, len(ids)+len(excluded))
	for _, id := range excluded {
		seen[id] = true
	}
	var result []string
	for _, id := range ids {
		if id != "" && !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}

func normalizeEnglishReview(e *tgstates.EnglishContext) {
	// Queues opened before this change may contain the current card reinserted
	// after three positions. Preserve the displayed card and remove that repeat.
	if e.ReviewTotal == 0 && e.Current != nil {
		e.ReviewSeen = englishUniqueIDs(append(e.ReviewSeen, e.Current.Id), nil)
		if e.Graded && e.Current.Streak == 0 && e.Current.Lapses > 0 {
			e.ReviewMissed = append(e.ReviewMissed, e.Current.Id)
		}
	}
	e.ReviewSeen = englishUniqueIDs(e.ReviewSeen, nil)
	e.Queue = englishUniqueIDs(e.Queue, e.ReviewSeen)
	e.ReviewMissed = englishUniqueIDs(e.ReviewMissed, nil)
	e.ReviewTotal = len(e.ReviewSeen) + len(e.Queue)
}

func (a *App) nextEnglishCard(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext) error {
	latest, err := a.English.ListCards(ctx, &pb.ListCardsRequest{})
	if err != nil {
		return err
	}
	byID := map[string]*pb.Card{}
	for _, c := range latest.Cards {
		byID[c.Id] = c
	}
	normalizeEnglishReview(&e)
	// Manual deletions cannot strand a pass or offer a nonexistent missed card.
	available := func(ids []string) []string {
		var result []string
		for _, id := range ids {
			if byID[id] != nil {
				result = append(result, id)
			}
		}
		return result
	}
	e.Queue = available(e.Queue)
	e.ReviewMissed = available(e.ReviewMissed)
	e.ReviewTotal = len(e.ReviewSeen) + len(e.Queue)
	e.Current = nil
	if len(e.Queue) > 0 {
		e.Current = byID[e.Queue[0]]
		e.Queue = e.Queue[1:]
		e.ReviewSeen = append(e.ReviewSeen, e.Current.Id)
	}
	e.Nonce = englishNonce()
	e.Revealed = false
	e.Graded = false
	if err = a.setEnglish(ctx, user, tgstates.StateEnglishReview, e); err != nil {
		return err
	}
	if e.Current == nil {
		if len(e.ReviewSeen) == 0 {
			return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishEmpty), tgkeyboards.EnglishBack())
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishPassFinished(len(e.ReviewSeen), len(e.ReviewMissed))), tgkeyboards.EnglishReviewComplete(e.Nonce, len(e.ReviewMissed)))
	}
	return replyToCallback(ctx, b, q, englishReviewFront(e), tgkeyboards.EnglishCard(e.Nonce, false, false))
}

func englishFront(c *pb.Card) tgfmt.HTML {
	date := r.EnglishUndated
	if c.Date != "" {
		date = "[[" + c.Date + "]]"
	}
	return tgfmt.Join(tgfmt.Escape(r.EnglishReviewFront), tgfmt.Blockquote(tgfmt.Escape(c.Source)), tgfmt.Escape("\n\n"+date))
}
func englishReviewPosition(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Escape(r.EnglishPassPosition(len(e.ReviewSeen), e.ReviewTotal) + "\n\n")
}
func englishReviewFront(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Join(englishReviewPosition(e), englishFront(e.Current))
}
func englishReviewAnswer(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Join(englishReviewPosition(e), englishAnswerText(e.Current))
}

func (a *App) englishReviewAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext, parts []string) error {
	normalizeEnglishReview(&e)
	if parts[1] == "missed" {
		if len(parts) != 3 || e.Current != nil || len(e.ReviewMissed) == 0 {
			return nil
		}
		return a.nextEnglishCard(ctx, b, q, user, tgstates.EnglishContext{Queue: slices.Clone(e.ReviewMissed), ReviewTotal: len(e.ReviewMissed)})
	}
	c := e.Current
	if c == nil {
		return nil
	}
	switch parts[1] {
	case "skip":
		if !e.Graded {
			return a.nextEnglishCard(ctx, b, q, user, e)
		}
	case "next":
		if e.Graded {
			return a.nextEnglishCard(ctx, b, q, user, e)
		}
	case "hint":
		if e.Revealed || e.Graded {
			return nil
		}
		first := []rune(c.English)
		if len(first) == 0 {
			return nil
		}
		return replyToCallback(ctx, b, q, tgfmt.Join(englishReviewFront(e), tgfmt.Escape("\n\n"+r.EnglishHintLabel+string(first[:1])+"…")), tgkeyboards.EnglishCard(e.Nonce, false, false))
	case "show":
		if e.Graded {
			return nil
		}
		e.Revealed = true
		if err := a.setEnglish(ctx, user, tgstates.StateEnglishReview, e); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, englishReviewAnswer(e), tgkeyboards.EnglishCard(e.Nonce, true, false))
	case "grade":
		if !e.Revealed || e.Graded || len(parts) != 4 {
			return nil
		}
		result, err := a.English.GradeCard(ctx, &pb.GradeCardRequest{Id: c.Id, Revision: c.Revision, Grade: parts[3]})
		if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.NotFound {
			return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishStale), tgkeyboards.EnglishBack())
		}
		if err != nil {
			return replyToCallback(ctx, b, q, tgfmt.Join(tgfmt.Escape(r.EnglishGradeFailure+"\n\n"), englishReviewAnswer(e)), tgkeyboards.EnglishCard(e.Nonce, true, false))
		}
		e.Current = result.Card
		e.Graded = true
		if parts[3] == "miss" {
			e.ReviewMissed = englishUniqueIDs(append(e.ReviewMissed, c.Id), nil)
		}
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishReview, e); err != nil {
			return err
		}
		next := time.Unix(result.Card.NextAt, 0).In(timeutil.FixedZone(a.Cfg.TimezoneOffsetHours)).Format("02-Jan-2006 15:04")
		return replyToCallback(ctx, b, q, tgfmt.Join(englishReviewPosition(e), tgfmt.Escape(fmt.Sprintf(r.EnglishReviewSaved, next, result.Card.Streak, r.EnglishInterval(result.Card.IntervalSeconds), result.Card.Lapses))), tgkeyboards.EnglishCard(e.Nonce, true, true))
	}
	return nil
}
