package tghandlers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

const browseNotePreviewMaxChars = 3500

func (a *App) HandleMenuBrowse(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateBrowseView
		u.BrowsePath = ""
	})
	return a.showBrowseFolder(ctx, tgBot, query, userID, "")
}

func (a *App) handleBrowseAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("browse.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "root":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateBrowseView
			u.BrowsePath = ""
		})
		return a.showBrowseFolder(ctx, tgBot, query, userID, "")

	case "up":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		parent := filepath.Dir(uc.BrowsePath)
		if parent == "." {
			parent = ""
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateBrowseView
			u.BrowsePath = parent
		})
		return a.showBrowseFolder(ctx, tgBot, query, userID, parent)

	case "open":
		if len(parts) < 3 {
			return nil
		}
		idx := 0
		fmt.Sscanf(parts[2], "%d", &idx)
		return a.handleBrowseOpenByIndex(ctx, tgBot, query, userID, idx)

	case "page":
		if len(parts) < 3 {
			return nil
		}
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		page := 0
		fmt.Sscanf(parts[2], "%d", &page)
		return a.showBrowseFolderAtPage(ctx, tgBot, query, userID, uc.BrowsePath, page)

	case "noop":
		return nil

	case "back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateIdle
			u.BrowsePath = ""
		})
		return a.showMainMenu(ctx, tgBot, query, userID)

	case "file_back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateBrowseView
		})
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		return a.showBrowseFolder(ctx, tgBot, query, userID, uc.BrowsePath)
	}
	return nil
}

func (a *App) handleBrowseOpenByIndex(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, idx int) error {
	ctx, span := telemetry.StartSpan(ctx, attribute.Int("browse.idx", idx))
	defer span.End()

	log := applog.With(ctx, a.Logger)

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		return fmt.Errorf("get context: %w", err)
	}

	entries, err := a.Core.ListDirectory(ctx, uc.BrowsePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Error("failed to list directory", zap.String("path", uc.BrowsePath), zap.Error(err))
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке."), nil)
	}

	if idx < 0 || idx >= len(entries) {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Элемент не найден."), nil)
	}

	relpath := entries[idx].Relpath

	content, readErr := a.Core.GetNoteByPath(ctx, relpath)
	if readErr == nil && content != "" {
		return a.showBrowseFile(ctx, tgBot, query, userID, relpath, content)
	}

	_, listErr := a.Core.ListDirectory(ctx, relpath)
	if listErr != nil {
		span.RecordError(listErr)
		span.SetStatus(codes.Error, listErr.Error())
		log.Error("failed to open path", zap.String("relpath", relpath), zap.Error(listErr))
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Не удалось открыть путь."), nil)
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateBrowseView
		u.BrowsePath = relpath
	})
	return a.showBrowseFolder(ctx, tgBot, query, userID, relpath)
}

func (a *App) showBrowseFolder(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, relpath string) error {
	return a.showBrowseFolderAtPage(ctx, tgBot, query, userID, relpath, 0)
}

func (a *App) showBrowseFolderAtPage(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, relpath string, page int) error {
	ctx, span := telemetry.StartSpan(ctx, attribute.String("browse.path", relpath))
	defer span.End()

	log := applog.With(ctx, a.Logger)

	entries, err := a.Core.ListDirectory(ctx, relpath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Error("failed to list directory", zap.String("relpath", relpath), zap.Error(err))
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке содержимого."), nil)
	}

	var headerText string
	if relpath == "" {
		headerText = "📂 Корень хранилища"
	} else {
		headerText = fmt.Sprintf("📂 %s", relpath)
	}

	kb := tgkeyboards.BrowseFolder(entries, relpath, page)
	text := tgfmt.Escape(headerText)

	if len(entries) == 0 {
		text = tgfmt.Join(
			tgfmt.Escape(headerText),
			tgfmt.Escape("\n\nПапка пуста."),
		)
	}

	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showBrowseFile(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, relpath string, content string) error {
	ctx, span := telemetry.StartSpan(ctx, attribute.String("browse.file", relpath))
	defer span.End()

	log := applog.With(ctx, a.Logger)

	if !utf8.ValidString(content) {
		log.Warn("file content has invalid UTF-8, sanitizing", zap.String("relpath", relpath))
		content = strings.ToValidUTF8(content, "")
	}

	fileName := filepath.Base(relpath)
	if len(content) > browseNotePreviewMaxChars {
		content = content[:browseNotePreviewMaxChars] + "\n\n_... обрезано_"
	}

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "browse:file_back"),
		),
	)

	text := tgfmt.Join(
		tgfmt.Escape("📄 "),
		tgfmt.Code(tgfmt.Escape(fileName)),
		tgfmt.Raw("\n"),
		tgfmt.Escape(relpath),
		tgfmt.Raw("\n\n"),
		tgfmt.Blockquote(tgfmt.Escape(content)),
	)

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateBrowseFile
		u.ActiveRelpath = relpath
	})

	return replyToCallback(ctx, tgBot, query, text, &kb)
}
