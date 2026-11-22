package tghandlers

import (
	"context"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/internal/noteimage"
)

// Telegram cannot render private vault URLs. Upload each embedded preview in the owner's private chat.
func (a *App) replyNotePage(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, text tgfmt.HTML, kb *telego.InlineKeyboardMarkup, note, page string) error {
	if err := replyToCallback(ctx, b, q, text, kb); err != nil {
		return err
	}
	reader, ok := a.Core.(clients.NoteImageReader)
	if !ok || q.Message == nil || q.Message.GetChat().ID != a.Cfg.RootID {
		return nil
	}
	seen := map[string]bool{}
	for _, link := range noteimage.Parse(page) {
		if seen[link.Reference] {
			continue
		}
		seen[link.Reference] = true
		img, err := reader.ReadNoteImage(ctx, note, link.Reference)
		if err != nil {
			continue
		}
		if _, err := b.SendPhoto(ctx, &telego.SendPhotoParams{ChatID: tu.ID(q.Message.GetChat().ID), Photo: tu.FileFromBytes(img.Data, "preview.jpg")}); err != nil {
			return err
		}
	}
	return nil
}
