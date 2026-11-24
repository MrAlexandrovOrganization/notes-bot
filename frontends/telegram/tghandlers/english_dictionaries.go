package tghandlers

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/english/v1"
)

func (a *App) chooseMultipleEnglishDictionaries(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64) error {
	result, err := a.English.ListDictionaries(ctx, &pb.ListDictionariesRequest{})
	if err != nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	choices := slices.DeleteFunc(result.Dictionaries, func(d *pb.DictionaryInfo) bool { return d.Id == "all" })
	return a.renderEnglishMulti(ctx, b, q, user, tgstates.EnglishContext{ScopeChoices: choices, Nonce: englishNonce()})
}

func (a *App) renderEnglishMulti(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext) error {
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishMenu, e); err != nil {
		return err
	}
	return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishSelectDictionaries),
		tgkeyboards.EnglishMultiDictionaries(e.Nonce, e.ScopeChoices, e.SelectedScopes))
}

const englishHiddenPageSize = 10

func (a *App) showHiddenEnglishCards(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, page int) error {
	result, err := a.English.ListCards(ctx, &pb.ListCardsRequest{HiddenOnly: true})
	if err != nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	cards := result.GetCards()
	page = max(0, min(page, (len(cards)-1)/englishHiddenPageSize))
	start := page * englishHiddenPageSize
	end := min(start+englishHiddenPageSize, len(cards))
	e := tgstates.EnglishContext{Nonce: englishNonce(), HiddenPage: page}
	for _, card := range cards[start:end] {
		e.HiddenCardIDs = append(e.HiddenCardIDs, card.Id)
	}
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishMenu, e); err != nil {
		return err
	}
	label := r.EnglishHiddenList
	if len(cards) == 0 {
		label = r.EnglishNoHiddenCards
	}
	return replyToCallback(ctx, b, q, tgfmt.Escape(label), tgkeyboards.EnglishHidden(e.Nonce, cards[start:end], start > 0, end < len(cards)))
}

func englishMenuIndex(parts []string, length int) (int, bool) {
	if len(parts) != 4 {
		return 0, false
	}
	index, err := strconv.Atoi(parts[3])
	return index, err == nil && index >= 0 && index < length
}

func (a *App) englishDictionaryMenuAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, e tgstates.EnglishContext, parts []string) error {
	switch parts[1] {
	case "pick":
		i, ok := englishMenuIndex(parts, len(e.ScopeChoices))
		if !ok {
			return nil
		}
		id := e.ScopeChoices[i].Id
		if j := slices.Index(e.SelectedScopes, id); j >= 0 {
			e.SelectedScopes = slices.Delete(e.SelectedScopes, j, j+1)
		} else {
			e.SelectedScopes = append(e.SelectedScopes, id)
		}
		e.Nonce = englishNonce()
		return a.renderEnglishMulti(ctx, b, q, user, e)
	case "use":
		if len(e.SelectedScopes) != 0 {
			return a.resumeEnglishReview(ctx, b, q, user, strings.Join(e.SelectedScopes, "+"))
		}
	case "hiddenprev", "hiddennext":
		if len(e.HiddenCardIDs) != 0 {
			page := e.HiddenPage + 1
			if parts[1] == "hiddenprev" {
				page = e.HiddenPage - 1
			}
			return a.showHiddenEnglishCards(ctx, b, q, user, page)
		}
	case "restore":
		i, ok := englishMenuIndex(parts, len(e.HiddenCardIDs))
		if !ok {
			return nil
		}
		_, err := a.English.SetCardHidden(ctx, &pb.SetCardHiddenRequest{
			CardId: e.HiddenCardIDs[i], OperationId: "telegram-visibility:" + e.Nonce + ":" + strconv.Itoa(i),
		})
		if err != nil && status.Code(err) != codes.NotFound {
			return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishVisibilityFailure), tgkeyboards.EnglishBack())
		}
		return a.showHiddenEnglishCards(ctx, b, q, user, e.HiddenPage)
	}
	return nil
}
