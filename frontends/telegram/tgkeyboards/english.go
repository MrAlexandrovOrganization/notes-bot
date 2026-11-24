package tgkeyboards

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	r "notes-bot/frontends/telegram/resources"
	pb "notes-bot/proto/english/v1"
)

func EnglishMenu() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishAdd).WithCallbackData("english:start")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishReview).WithCallbackData("english:review")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishHiddenCards).WithCallbackData("english:hidden")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("menu:back")),
	)
}
func EnglishBack() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("english:menu")))
}

func EnglishDictionaries(dictionaries []*pb.DictionaryInfo) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, len(dictionaries)+1)
	for _, dictionary := range dictionaries {
		label := r.EnglishDictionaryChoice(dictionary.Title, dictionary.CardCount)
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(label).WithCallbackData("english:dict:"+dictionary.Id)))
	}
	if len(dictionaries) > 2 {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishChooseSeveral).WithCallbackData("english:multi")))
	}
	rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("english:menu")))
	return tu.InlineKeyboard(rows...)
}

func EnglishMultiDictionaries(nonce string, dictionaries []*pb.DictionaryInfo, selected []string) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, len(dictionaries)+2)
	for i, dictionary := range dictionaries {
		label := r.EnglishDictionaryChoice(dictionary.Title, dictionary.CardCount)
		if slices.Contains(selected, dictionary.Id) {
			label = "✓ " + label
		}
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(label).
			WithCallbackData("english:pick:"+nonce+":"+strconv.Itoa(i))))
	}
	if len(selected) != 0 {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishStartSelection).
			WithCallbackData("english:use:"+nonce)))
	}
	rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("english:review")))
	return tu.InlineKeyboard(rows...)
}

func EnglishHidden(nonce string, cards []*pb.Card, previous, next bool) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, len(cards)+2)
	for i, card := range cards {
		label := []rune(strings.Join(strings.Fields(card.English), " "))
		if len(label) > 46 {
			label = append(label[:45], '…')
		}
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(string(label)).
			WithCallbackData("english:restore:"+nonce+":"+strconv.Itoa(i))))
	}
	var pages []telego.InlineKeyboardButton
	if previous {
		pages = append(pages, tu.InlineKeyboardButton(r.EnglishPreviousPage).WithCallbackData("english:hiddenprev:"+nonce))
	}
	if next {
		pages = append(pages, tu.InlineKeyboardButton(r.EnglishNextPage).WithCallbackData("english:hiddennext:"+nonce))
	}
	if len(pages) > 0 {
		rows = append(rows, pages)
	}
	rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("english:menu")))
	return tu.InlineKeyboard(rows...)
}
func EnglishPending(nonce string) *telego.InlineKeyboardMarkup {
	kb := EnglishBack()
	kb.InlineKeyboard = append([][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishGenerate).WithCallbackData("english:generate:" + nonce)),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishNew).WithCallbackData("english:new:" + nonce)),
	}, kb.InlineKeyboard...)
	return kb
}

func EnglishPreview(nonce string, ready, manual bool) *telego.InlineKeyboardMarkup {
	kb := EnglishBack()
	rows := [][]telego.InlineKeyboardButton{}
	if ready {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishSave).WithCallbackData("english:save:"+nonce)))
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishExample).WithCallbackData("english:example:"+nonce), tu.InlineKeyboardButton(r.EnglishDetail).WithCallbackData("english:detail:"+nonce)))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishRetry).WithCallbackData("english:retry:"+nonce)))
	}
	newInput := tu.InlineKeyboardButton(r.EnglishNew).WithCallbackData("english:new:" + nonce)
	if manual {
		rows = append(rows, tu.InlineKeyboardRow(newInput))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishClarify).WithCallbackData("english:clarify:"+nonce), newInput))
	}
	kb.InlineKeyboard = append(rows, kb.InlineKeyboard...)
	return kb
}
func EnglishCard(nonce string, revealed bool) *telego.InlineKeyboardMarkup {
	var actions []telego.InlineKeyboardButton
	if revealed {
		actions = tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(r.EnglishMiss).WithCallbackData("english:grade:"+nonce+":miss"),
			tu.InlineKeyboardButton(r.EnglishHard).WithCallbackData("english:grade:"+nonce+":hard"),
			tu.InlineKeyboardButton(r.EnglishEasy).WithCallbackData("english:grade:"+nonce+":easy"),
		)
	} else {
		actions = tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(r.EnglishShow).WithCallbackData("english:show:"+nonce),
			tu.InlineKeyboardButton(r.EnglishHint).WithCallbackData("english:hint:"+nonce),
		)
	}
	return tu.InlineKeyboard(
		actions,
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(r.EnglishSkip).WithCallbackData("english:skip:"+nonce),
			tu.InlineKeyboardButton(r.EnglishHideCard).WithCallbackData("english:hide:"+nonce),
		),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishFinish).WithCallbackData("english:menu")),
	)
}
