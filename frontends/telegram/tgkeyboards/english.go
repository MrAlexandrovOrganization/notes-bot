package tgkeyboards

import (
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	r "notes-bot/frontends/telegram/resources"
)

func EnglishMenu() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishAdd).WithCallbackData("english:start")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishReview).WithCallbackData("english:review")),
		tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("menu:back")),
	)
}
func EnglishBack() *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishBack).WithCallbackData("english:menu")))
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
func EnglishReviewStart() *telego.InlineKeyboardMarkup {
	kb := EnglishBack()
	kb.InlineKeyboard = append([][]telego.InlineKeyboardButton{tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishStartReview).WithCallbackData("english:queue"))}, kb.InlineKeyboard...)
	return kb
}
func EnglishCard(nonce string, revealed, graded bool) *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{}
	if graded {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishNext).WithCallbackData("english:next:"+nonce)))
	} else if revealed {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishMiss).WithCallbackData("english:grade:"+nonce+":miss"), tu.InlineKeyboardButton(r.EnglishHard).WithCallbackData("english:grade:"+nonce+":hard"), tu.InlineKeyboardButton(r.EnglishEasy).WithCallbackData("english:grade:"+nonce+":easy")))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishShow).WithCallbackData("english:show:"+nonce), tu.InlineKeyboardButton(r.EnglishHint).WithCallbackData("english:hint:"+nonce)))
	}
	if !graded {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishSkip).WithCallbackData("english:skip:"+nonce)))
	}
	rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishFinish).WithCallbackData("english:menu")))
	return tu.InlineKeyboard(rows...)
}

func EnglishReviewComplete(nonce string, missed int) *telego.InlineKeyboardMarkup {
	rows := [][]telego.InlineKeyboardButton{}
	if missed > 0 {
		rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishRepeatMissed(missed)).WithCallbackData("english:missed:"+nonce)))
	}
	rows = append(rows, tu.InlineKeyboardRow(tu.InlineKeyboardButton(r.EnglishFinish).WithCallbackData("english:menu")))
	return tu.InlineKeyboard(rows...)
}
