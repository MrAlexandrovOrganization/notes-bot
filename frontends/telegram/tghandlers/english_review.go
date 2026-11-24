package tghandlers

import (
	"context"

	"github.com/mymmrac/telego"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/english/v1"
)

func (a *App) chooseEnglishDictionary(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64) error {
	result, err := a.English.ListDictionaries(ctx, &pb.ListDictionariesRequest{})
	if status.Code(err) == codes.Unimplemented {
		return a.resumeEnglishReview(ctx, b, q, user, "")
	}
	if err != nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishMenu, tgstates.EnglishContext{}); err != nil {
		return err
	}
	return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishChooseDictionary), tgkeyboards.EnglishDictionaries(result.Dictionaries))
}

func (a *App) resumeEnglishReview(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, dictionary string) error {
	result, err := a.English.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: dictionary})
	if dictionary != "" && status.Code(err) == codes.NotFound {
		return a.chooseEnglishDictionary(ctx, b, q, user)
	}
	if err != nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	return a.renderEnglishReview(ctx, b, q, user, result.GetCurrent(), "")
}

func (a *App) renderEnglishReview(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, current *pb.ReviewPosition, notice string) error {
	if current.GetCard() == nil {
		if err := a.setEnglish(ctx, user, tgstates.StateEnglishMenu, tgstates.EnglishContext{}); err != nil {
			return err
		}
		text := r.EnglishEmpty
		if notice != "" {
			text = notice + "\n\n" + text
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(text), tgkeyboards.EnglishBack())
	}
	e := tgstates.EnglishContext{Review: current, Nonce: englishNonce()}
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishReview, e); err != nil {
		return err
	}
	text := englishReviewFront(e)
	if notice != "" {
		text = tgfmt.Join(tgfmt.Escape(notice+"\n\n"), text)
	}
	return replyToCallback(ctx, b, q, text, tgkeyboards.EnglishCard(e.Nonce, false))
}

func englishFront(position *pb.ReviewPosition) tgfmt.HTML {
	c := position.Card
	cue, label := c.Source, r.EnglishReviewFront
	if position.PromptLanguage == "en" {
		cue, label = c.English, "Вспомни по-русски:\n\n"
	}
	date := r.EnglishUndated
	if c.Date != "" {
		date = "[[" + c.Date + "]]"
	}
	return tgfmt.Join(tgfmt.Escape(label), tgfmt.Blockquote(tgfmt.Escape(cue)), tgfmt.Escape("\n\n"+date))
}

func englishReviewPosition(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Escape(r.EnglishRoundPosition(e.Review.GetRound(), e.Review.GetPosition(), e.Review.GetRoundSize()) + "\n\n")
}

func englishReviewFront(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Join(englishReviewPosition(e), englishFront(e.Review))
}

func englishReviewAnswer(e tgstates.EnglishContext) tgfmt.HTML {
	return tgfmt.Join(englishReviewPosition(e), englishAnswerText(e.Review.Card))
}

func (a *App) englishReviewAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext, parts []string) error {
	if e.Review.GetCard() == nil || e.Review.PromptLanguage == "" {
		// The previous release stored review queues entirely in Telegram.
		return a.resumeEnglishReview(ctx, b, q, user, e.Review.GetDictionaryId())
	}
	switch parts[1] {
	case "hint":
		if e.Revealed {
			return nil
		}
		answer := e.Review.Card.English
		if e.Review.PromptLanguage == "en" {
			answer = e.Review.Card.Source
		}
		first := []rune(answer)
		if len(first) == 0 {
			return nil
		}
		return replyToCallback(ctx, b, q, tgfmt.Join(englishReviewFront(e), tgfmt.Escape("\n\n"+r.EnglishHintLabel+string(first[:1])+"…")), tgkeyboards.EnglishCard(e.Nonce, false))
	case "show":
		e.Revealed = true
		if err := a.setEnglish(ctx, user, tgstates.StateEnglishReview, e); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, englishReviewAnswer(e), tgkeyboards.EnglishCard(e.Nonce, true))
	case "skip":
		return a.advanceEnglishReview(ctx, b, q, user, e, pb.ReviewAction_REVIEW_ACTION_SKIP)
	case "hide":
		return a.hideEnglishReviewCard(ctx, b, q, user, e)
	case "grade":
		if !e.Revealed || len(parts) != 4 {
			return nil
		}
		switch parts[3] {
		case "miss":
			return a.advanceEnglishReview(ctx, b, q, user, e, pb.ReviewAction_REVIEW_ACTION_MISS)
		case "hard":
			return a.advanceEnglishReview(ctx, b, q, user, e, pb.ReviewAction_REVIEW_ACTION_HARD)
		case "easy":
			return a.advanceEnglishReview(ctx, b, q, user, e, pb.ReviewAction_REVIEW_ACTION_EASY)
		}
	}
	return nil
}

func (a *App) hideEnglishReviewCard(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext) error {
	_, err := a.English.SetCardHidden(ctx, &pb.SetCardHiddenRequest{
		CardId: e.Review.GetCard().GetId(), Hidden: true, OperationId: "telegram-visibility:" + e.Nonce,
	})
	if status.Code(err) == codes.NotFound {
		return a.resumeEnglishReview(ctx, b, q, user, e.Review.GetDictionaryId())
	}
	if err != nil {
		card := englishReviewFront(e)
		if e.Revealed {
			card = englishReviewAnswer(e)
		}
		return replyToCallback(ctx, b, q, tgfmt.Join(tgfmt.Escape(r.EnglishVisibilityFailure+"\n\n"), card), tgkeyboards.EnglishCard(e.Nonce, e.Revealed))
	}
	result, err := a.English.GetReview(ctx, &pb.GetReviewRequest{DictionaryId: e.Review.GetDictionaryId()})
	if err != nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	return a.renderEnglishReview(ctx, b, q, user, result.GetCurrent(), r.EnglishCardHidden)
}

func (a *App) advanceEnglishReview(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext, action pb.ReviewAction) error {
	result, err := a.English.AdvanceReview(ctx, &pb.AdvanceReviewRequest{
		Token: e.Review.Token, Action: action, DictionaryId: e.Review.GetDictionaryId(),
	})
	if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.NotFound {
		return a.resumeEnglishReview(ctx, b, q, user, e.Review.GetDictionaryId())
	}
	if err != nil {
		card := englishReviewFront(e)
		if e.Revealed {
			card = englishReviewAnswer(e)
		}
		return replyToCallback(ctx, b, q, tgfmt.Join(tgfmt.Escape(r.EnglishGradeFailure+"\n\n"), card), tgkeyboards.EnglishCard(e.Nonce, e.Revealed))
	}
	notice := ""
	if action != pb.ReviewAction_REVIEW_ACTION_SKIP {
		notice = r.EnglishReviewSaved(result.SkippedRounds)
	}
	return a.renderEnglishReview(ctx, b, q, user, result.GetCurrent(), notice)
}
