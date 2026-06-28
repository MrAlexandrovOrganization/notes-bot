package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
)

const notePreviewMaxChars = 3800

var callbackActionHandlers = map[string]func(*App, context.Context, *tgbotapi.BotAPI, *tgbotapi.CallbackQuery, int64, []string) error{
	"menu":     (*App).handleMenuAction,
	"task":     (*App).handleTaskAction,
	"cal":      (*App).handleCalAction,
	"reminder": (*App).handleReminderAction,
	"note":     (*App).handleNoteAction,
	"voice":    (*App).handleVoiceAction,
	"smart":    (*App).handleSmartAction,
	"find":     (*App).handleFindAction,
	"browse":   (*App).handleBrowseAction,
}

func (a *App) HandleCallback(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	query := update.CallbackQuery
	if query == nil || query.Data == "" || query.From == nil {
		return
	}

	ctx, span := telemetry.StartSpan(ctx, attribute.String("callback.data", query.Data))
	defer span.End()

	log := applog.With(ctx, a.Logger)
	go tgBot.Request(tgbotapi.NewCallback(query.ID, ""))

	userID := query.From.ID
	if !a.authorized(userID) {
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("⛔ Unauthorized access."), nil)
		log.Warn("unauthorized callback", zap.Int64("user_id", userID))
		return
	}

	parts := strings.Split(query.Data, ":")
	if len(parts) == 0 {
		return
	}

	action := parts[0]

	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in callback handler", zap.Any("recover", r), zap.String("data", query.Data), zap.String("stack", string(debug.Stack())))
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла внутренняя ошибка."), nil)
		}
	}()

	var err error
	if h, ok := callbackActionHandlers[action]; ok {
		err = h(a, ctx, tgBot, query, userID, parts)
	} else {
		log.Warn("unknown callback action", zap.String("action", action))
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается. Попробуйте через несколько секунд."), nil)
			return
		}
		log.Error("callback error", zap.String("data", query.Data), zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка при обработке действия."), nil)
	}
}

// ── Menu ──────────────────────────────────────────────────────────────────

func (a *App) handleMenuAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("menu.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "rating":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		currentRating, hasRating, _ := a.Core.GetRating(ctx, uc.ActiveDate)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingRating })
		kb := tgkeyboards.RatingPrompt(hasRating, currentRating)
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("📊 Введите оценку дня (0-10):"), &kb)

	case "back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		return a.showMainMenu(ctx, tgBot, query, userID)

	case "noop":
		return nil

	case "tasks":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateTasksView
			u.TaskPage = 0
		})
		go a.Core.EnsureNote(ctx, uc.ActiveDate)
		return a.showTasks(ctx, tgBot, query, userID)

	case "note":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.NotePage = 0 })
		return a.showNote(ctx, tgBot, query, userID)

	case "calendar":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateCalendarView })
		return a.showCalendar(ctx, tgBot, query, userID)

	case "notifications":
		a.HandleMenuNotifications(ctx, tgBot, query, userID)

	case "smart":
		a.HandleSmartStart(ctx, tgBot, query, userID)

	case "find":
		return a.HandleMenuFind(ctx, tgBot, query, userID)

	case "ask":
		return a.HandleMenuAsk(ctx, tgBot, query, userID)

	case "browse":
		return a.HandleMenuBrowse(ctx, tgBot, query, userID)
	}
	return nil
}

// ── Smart router ──────────────────────────────────────────────────────────

func (a *App) handleSmartAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("smart.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "yes":
		a.HandleSmartConfirm(ctx, tgBot, query, userID)
	case "no":
		a.HandleSmartReject(ctx, tgBot, query, userID)
	case "pick":
		if len(parts) >= 3 {
			a.HandleSmartPickIntent(ctx, tgBot, query, userID, parts[2])
		}
	}
	return nil
}

// ── Tasks ─────────────────────────────────────────────────────────────────

func (a *App) handleTaskAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("task.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "toggle":
		if len(parts) < 3 {
			return nil
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("parse task index: %w", err)
		}
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		if ok, _ := a.Core.ToggleTask(ctx, uc.ActiveDate, idx); ok {
			return a.showTasks(ctx, tgBot, query, userID)
		}
		go tgBot.Request(tgbotapi.NewCallbackWithAlert(query.ID, "❌ Ошибка при переключении задачи"))

	case "add":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingNewTask })
		kb := tgkeyboards.TaskAdd()
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("➕ Введите текст новой задачи:"), &kb)

	case "page":
		if len(parts) < 3 {
			return nil
		}
		page, _ := strconv.Atoi(parts[2])
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.TaskPage = page })
		return a.showTasks(ctx, tgBot, query, userID)

	case "back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		return a.showMainMenu(ctx, tgBot, query, userID)

	case "cancel":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateTasksView })
		return a.showTasks(ctx, tgBot, query, userID)

	case "noop":
	}
	return nil
}

// ── Calendar ──────────────────────────────────────────────────────────────

func (a *App) handleCalAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("cal.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "prev":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		month, year := stepMonth(uc.CalendarMonth, uc.CalendarYear, -1)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.CalendarMonth = month
			u.CalendarYear = year
		})
		return a.showCalendar(ctx, tgBot, query, userID)

	case "next":
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			return fmt.Errorf("get context: %w", err)
		}
		month, year := stepMonth(uc.CalendarMonth, uc.CalendarYear, 1)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.CalendarMonth = month
			u.CalendarYear = year
		})
		return a.showCalendar(ctx, tgBot, query, userID)

	case "select":
		if len(parts) < 3 {
			return nil
		}
		date := parts[2]
		go a.Core.EnsureNote(ctx, date)
		a.State.SetActiveDate(ctx, userID, date)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		text := tgfmt.Join(
			tgfmt.Escape("✅ Выбрана дата: "),
			tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", date))),
			tgfmt.Escape("\n\n📅 Активная дата: "),
			tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", date))),
			tgfmt.Escape("\n\nВыберите действие:"),
		)
		kb := a.getMainMenuKeyboard(ctx)
		return replyToCallback(ctx, tgBot, query, text, &kb)

	case "today":
		todayDate, err := a.Core.GetTodayDate(ctx)
		if err != nil {
			return fmt.Errorf("get today date: %w", err)
		}
		a.State.SetActiveDate(ctx, userID, todayDate)
		now := time.Now()
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.CalendarMonth = int(now.Month())
			u.CalendarYear = now.Year()
		})
		return a.showCalendar(ctx, tgBot, query, userID)

	case "back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		return a.showMainMenu(ctx, tgBot, query, userID)

	case "noop":
	}
	return nil
}

// ── Note ───────────────────────────────────────────────────────────────────

func (a *App) handleNoteAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	ctx, span := telemetry.StartSpan(ctx, attribute.String("note.action", parts[1]))
	defer span.End()

	switch parts[1] {
	case "page":
		if len(parts) < 3 {
			return nil
		}
		page, _ := strconv.Atoi(parts[2])
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.NotePage = page })
		return a.showNote(ctx, tgBot, query, userID)

	case "back":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		return a.showMainMenu(ctx, tgBot, query, userID)

	case "append":
		return a.handleNoteAppendAction(ctx, tgBot, query, userID)

	case "noop":
	}
	return nil
}

// ── Reminder ──────────────────────────────────────────────────────────────

func (a *App) handleReminderAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	sub := parts[1]

	ctx, span := telemetry.StartSpan(ctx, attribute.String("reminder.action", sub))
	defer span.End()

	switch sub {
	case "create":
		a.HandleReminderCreate(ctx, tgBot, query, userID)

	case "create_nl":
		a.HandleReminderCreateNL(ctx, tgBot, query, userID)

	case "nl_confirm":
		a.HandleReminderNLConfirm(ctx, tgBot, query, userID)

	case "page":
		if len(parts) >= 3 {
			page, _ := strconv.Atoi(parts[2])
			a.HandleReminderPage(ctx, tgBot, query, userID, page)
		}

	case "type":
		if len(parts) >= 3 {
			a.HandleReminderTypeSelect(ctx, tgBot, query, userID, parts[2])
		}

	case "task_confirm":
		if len(parts) >= 3 {
			a.HandleReminderTaskConfirm(ctx, tgBot, query, userID, parts[2] == "yes")
		}

	case "delete":
		if len(parts) >= 3 {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			a.HandleReminderDelete(ctx, tgBot, query, userID, id)
		}

	case "reject":
		if len(parts) >= 3 {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			a.HandleReminderReject(ctx, tgBot, query, userID, id)
		}

	case "postpone_input":
		if len(parts) >= 3 {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			a.HandleReminderPostponeInput(ctx, tgBot, query, userID, id)
		}

	case "done":
		if len(parts) >= 3 {
			reminderID, _ := strconv.ParseInt(parts[2], 10, 64)
			createTaskFlag := 0
			if len(parts) > 3 {
				createTaskFlag, _ = strconv.Atoi(parts[3])
			}
			dateStr := ""
			if len(parts) > 4 {
				dateStr = parts[4]
			}
			a.HandleReminderDone(ctx, tgBot, query, userID, reminderID, createTaskFlag, dateStr)
		}

	case "postpone_date":
		if len(parts) >= 3 {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			a.HandleReminderPostponeDate(ctx, tgBot, query, userID, id)
		}

	case "cal":
		if len(parts) >= 4 {
			calSub := parts[2]
			contextName := parts[3]
			switch calSub {
			case "prev":
				a.HandleReminderCalPrev(ctx, tgBot, query, userID, contextName)
			case "next":
				a.HandleReminderCalNext(ctx, tgBot, query, userID, contextName)
			case "today":
				a.HandleReminderCalToday(ctx, tgBot, query, userID, contextName)
			case "select":
				if len(parts) >= 5 {
					dateStr := parts[3]
					ctxName := parts[4]
					a.HandleReminderCalSelect(ctx, tgBot, query, userID, dateStr, ctxName)
				}
			}
		}

	case "back":
		a.HandleReminderBack(ctx, tgBot, query, userID)

	case "cancel":
		a.HandleReminderCancel(ctx, tgBot, query, userID)

	case "noop":
	}
	return nil
}

// ── Shared display helpers ─────────────────────────────────────────────────

func (a *App) showMainMenu(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		return fmt.Errorf("get context: %w", err)
	}
	text := tgfmt.Join(
		tgfmt.Escape("📅 Активная дата: "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
		tgfmt.Escape("\n\nВыберите действие:"),
	)
	kb := a.getMainMenuKeyboard(ctx)
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showTasks(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}
	tasks, err := a.Core.GetTasks(ctx, uc.ActiveDate)
	if err != nil {
		return fmt.Errorf("get tasks: %w", err)
	}
	kb := tgkeyboards.Tasks(tasks, uc.TaskPage)
	var text tgfmt.HTML

	header := tgfmt.Join(
		tgfmt.Escape("✅ Задачи на "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
		tgfmt.Escape(":\n\n"),
	)
	if len(tasks) == 0 {
		text = tgfmt.Join(
			header,
			tgfmt.Escape("Задач пока нет."),
		)
	} else {
		text = tgfmt.Join(
			header,
			tgfmt.Escape(fmt.Sprintf("Всего задач: %d", len(tasks))),
		)
	}
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showCalendar(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		return fmt.Errorf("get context: %w", err)
	}
	existingDatesList, err := a.Core.GetExistingDates(ctx)
	if err != nil {
		return fmt.Errorf("get existing dates: %w", err)
	}
	existingDates := make(map[string]bool, len(existingDatesList))
	for _, d := range existingDatesList {
		existingDates[d] = true
	}
	kb := tgkeyboards.Calendar(ctx, uc.CalendarYear, uc.CalendarMonth, uc.ActiveDate, existingDates)
	text := tgfmt.Join(
		tgfmt.Escape("📅 Календарь\n\nАктивная дата: "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
	)
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showNote(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("get context: %w", err)
	}
	activeDate := uc.ActiveDate
	currentPage := uc.NotePage

	var content string
	var rating int
	var hasRating bool
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		a.Core.EnsureNote(gCtx, activeDate)
		return nil
	})
	g.Go(func() error {
		var getErr error
		content, getErr = a.Core.GetNote(gCtx, activeDate)
		if getErr != nil {
			log.Warn("failed to get note content", zap.String("date", activeDate), zap.Error(getErr))
		}
		return nil
	})
	g.Go(func() error {
		var getErr error
		rating, hasRating, getErr = a.Core.GetRating(gCtx, activeDate)
		if getErr != nil {
			log.Warn("failed to get note rating", zap.String("date", activeDate), zap.Error(getErr))
		}
		return nil
	})
	g.Wait() //nolint:errcheck // errors are handled per-call inside goroutines above

	if content == "" {
		return replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Не удалось прочитать заметку."), nil)
	}

	if !utf8.ValidString(content) {
		log.Warn("note content has invalid UTF-8, sanitizing", zap.String("date", activeDate))
		content = strings.ToValidUTF8(content, "")
	}

	ratingText := "Оценка: не установлена"
	if hasRating {
		ratingText = fmt.Sprintf("Оценка: %d", rating)
	}

	pageContent, kb := tgkeyboards.NotePagination(content, currentPage)

	span.SetAttributes(
		attribute.String("date", activeDate),
		attribute.Int("page", currentPage),
		attribute.Int("content_len", len(pageContent)),
	)

	text := tgfmt.Join(
		tgfmt.Raw("📝 Заметка "),
		tgfmt.Code(tgfmt.Escape(activeDate)),
		tgfmt.Raw("\n\n"),
		tgfmt.Escape(ratingText),
		tgfmt.Raw("\n\n"),
		tgfmt.Blockquote(tgfmt.Escape(pageContent)),
	)
	return replyToCallback(ctx, tgBot, query, text, kb)
}

func stepMonth(month, year, delta int) (int, int) {
	month += delta
	if month < 1 {
		return 12, year - 1
	}
	if month > 12 {
		return 1, year + 1
	}
	return month, year
}
