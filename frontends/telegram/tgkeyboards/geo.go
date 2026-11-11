package tgkeyboards

import (
	"fmt"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"notes-bot/frontends/telegram/resources"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

func GeoButton(label, data string) []telego.InlineKeyboardButton {
	return tu.InlineKeyboardRow(tu.InlineKeyboardButton(label).WithCallbackData(data))
}
func GeoKeyboard(rows ...[]telego.InlineKeyboardButton) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(rows...)
}
func GeoCancel() *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.GeoCancel, "geo:list:0"))
}
func GeoPlaces(places []*loc.Place, page int) *telego.InlineKeyboardMarkup {
	const size = 6
	pages := max(1, (len(places)+size-1)/size)
	page = max(0, min(page, pages-1))
	var rows [][]telego.InlineKeyboardButton
	for _, p := range places[page*size : min(len(places), (page+1)*size)] {
		rows = append(rows, GeoButton(short(p.Name), "geo:place:"+p.Id))
	}
	if page > 0 {
		rows = append(rows, GeoButton("◀", fmt.Sprintf("geo:list:%d", page-1)))
	}
	if page+1 < pages {
		rows = append(rows, GeoButton("▶", fmt.Sprintf("geo:list:%d", page+1)))
	}
	rows = append(rows, GeoButton(resources.GeoNewPlace, "geo:new"), GeoButton(resources.GeoRules, "geo:rules:0"), GeoButton(resources.GeoBack, "menu:back"))
	return GeoKeyboard(rows...)
}
func GeoPlace(id string) *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.GeoEntered, "geo:rule:"+id+":entered"), GeoButton(resources.GeoExited, "geo:rule:"+id+":exited"), GeoButton(resources.GeoEdit, "geo:edit:"+id), GeoButton(resources.GeoDelete, "geo:delplace:"+id), GeoButton(resources.GeoBack, "geo:list:0"))
}
func GeoRepeat() *telego.InlineKeyboardMarkup {
	return GeoKeyboard(GeoButton(resources.GeoOnce, "geo:repeat:once"), GeoButton(resources.GeoAlways, "geo:repeat:always"), GeoButton(resources.GeoDaily, "geo:repeat:daily"), GeoButton(resources.GeoCancel, "geo:list:0"))
}
func GeoRules(rules []*pb.Rule, page int) *telego.InlineKeyboardMarkup {
	const size = 6
	pages := max(1, (len(rules)+size-1)/size)
	page = max(0, min(page, pages-1))
	var rows [][]telego.InlineKeyboardButton
	for _, r := range rules[page*size : min(len(rules), (page+1)*size)] {
		rows = append(rows, GeoButton(short(resources.GeoRuleLabel(r.Title, r.Armed)), fmt.Sprintf("geo:viewrule:%d", r.Id)))
	}
	if page > 0 {
		rows = append(rows, GeoButton("◀", fmt.Sprintf("geo:rules:%d", page-1)))
	}
	if page+1 < pages {
		rows = append(rows, GeoButton("▶", fmt.Sprintf("geo:rules:%d", page+1)))
	}
	rows = append(rows, GeoButton(resources.GeoBack, "geo:list:0"))
	return GeoKeyboard(rows...)
}
func short(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
