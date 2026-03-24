package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"notes_bot/frontends/telegram/clients"
	"notes_bot/frontends/telegram/tgkeyboards"
	"notes_bot/frontends/telegram/tgstates"
	"notes_bot/internal/applog"
	"notes_bot/internal/telemetry"
)

const notePreviewMaxChars = 3800

var callbackActionHandlers = map[string]func(*App, context.Context, *tgbotapi.BotAPI, *tgbotapi.CallbackQuery, int64, []string) error{
	"menu":     (*App).handleMenuAction,
	"task":     (*App).handleTaskAction,
	"cal":      (*App).handleCalAction,
	"reminder": (*App).handleReminderAction,
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
		replyToCallback(ctx, tgBot, query, "⛔ Unauthorized access.", nil)
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
		}
	}()

	var err error
	if h, ok := callbackActionHandlers[action]; ok {
		err = h(a, ctx, tgBot, query, userID, parts)
	} else {
		log.Warn("unknown callback action", zap.String("action", action))
	}

	if err != nil {
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			replyToCallback(ctx, tgBot, query, "⏳ Сервис уведомлений ещё запускается. Попробуйте через несколько секунд.", nil)
			return
		}
		log.Error("callback error", zap.String("data", query.Data), zap.Error(err))
		replyToCallback(ctx, tgBot, query, "❌ Произошла ошибка при обработке действия.", nil)
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
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingRating })
		return replyToCallback(ctx, tgBot, query, "📊 Введите оценку дня (0-10):", nil)

	case "tasks":
		uc, _ := a.State.GetContext(ctx, userID)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateTasksView
			u.TaskPage = 0
		})
		go a.Core.EnsureNote(ctx, uc.ActiveDate)
		return a.showTasks(ctx, tgBot, query, userID)

	case "note":
		return a.showNote(ctx, tgBot, query, userID)

	case "calendar":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateCalendarView })
		return a.showCalendar(ctx, tgBot, query, userID)

	case "notifications":
		a.HandleMenuNotifications(ctx, tgBot, query, userID)
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
		idx, _ := strconv.Atoi(parts[2])
		uc, _ := a.State.GetContext(ctx, userID)
		if ok, _ := a.Core.ToggleTask(ctx, uc.ActiveDate, idx); ok {
			return a.showTasks(ctx, tgBot, query, userID)
		}
		go tgBot.Request(tgbotapi.NewCallbackWithAlert(query.ID, "❌ Ошибка при переключении задачи"))

	case "add":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingNewTask })
		kb := tgkeyboards.TaskAdd()
		return replyToCallback(ctx, tgBot, query, "➕ Введите текст новой задачи:", &kb)

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
		uc, _ := a.State.GetContext(ctx, userID)
		month, year := stepMonth(uc.CalendarMonth, uc.CalendarYear, -1)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.CalendarMonth = month
			u.CalendarYear = year
		})
		return a.showCalendar(ctx, tgBot, query, userID)

	case "next":
		uc, _ := a.State.GetContext(ctx, userID)
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
		text := fmt.Sprintf("✅ Выбрана дата: %s\n\n📅 Активная дата: %s\n\nВыберите действие:",
			date, date)
		kb := a.getMainMenuKeyboard(ctx)
		return replyToCallback(ctx, tgBot, query, text, &kb)

	case "today":
		todayDate, _ := a.Core.GetTodayDate(ctx)
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

	case "postpone":
		if len(parts) >= 4 {
			days, _ := strconv.ParseInt(parts[2], 10, 64)
			reminderID, _ := strconv.ParseInt(parts[3], 10, 64)
			a.HandleReminderPostponeDays(ctx, tgBot, query, userID, days, reminderID)
		}

	case "postpone_hours":
		if len(parts) >= 4 {
			hours, _ := strconv.ParseInt(parts[2], 10, 64)
			reminderID, _ := strconv.ParseInt(parts[3], 10, 64)
			a.HandleReminderPostponeHours(ctx, tgBot, query, userID, hours, reminderID)
		}

	case "custom_date":
		if len(parts) >= 3 {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			a.HandleReminderCustomDate(ctx, tgBot, query, userID, id)
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

	uc, _ := a.State.GetContext(ctx, userID)
	text := fmt.Sprintf("📅 Активная дата: %s\n\nВыберите действие:", uc.ActiveDate)
	kb := a.getMainMenuKeyboard(ctx)
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showTasks(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, _ := a.State.GetContext(ctx, userID)
	tasks, _ := a.Core.GetTasks(ctx, uc.ActiveDate)
	kb := tgkeyboards.Tasks(tasks, uc.TaskPage)
	text := fmt.Sprintf("✅ Задачи на %s:\n\nВсего задач: %d", uc.ActiveDate, len(tasks))
	if len(tasks) == 0 {
		text = fmt.Sprintf("✅ Задачи на %s:\n\nЗадач пока нет.", uc.ActiveDate)
	}
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showCalendar(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, _ := a.State.GetContext(ctx, userID)
	existingDatesList, _ := a.Core.GetExistingDates(ctx)
	existingDates := make(map[string]bool, len(existingDatesList))
	for _, d := range existingDatesList {
		existingDates[d] = true
	}
	kb := tgkeyboards.Calendar(ctx, uc.CalendarYear, uc.CalendarMonth, uc.ActiveDate, existingDates)
	text := fmt.Sprintf("📅 Календарь\n\nАктивная дата: %s", uc.ActiveDate)
	return replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) showNote(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	uc, _ := a.State.GetContext(ctx, userID)
	activeDate := uc.ActiveDate

	var content string
	var rating int
	var hasRating bool
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		a.Core.EnsureNote(gCtx, activeDate)
		return nil
	})
	g.Go(func() error {
		content, _ = a.Core.GetNote(gCtx, activeDate)
		return nil
	})
	g.Go(func() error {
		rating, hasRating, _ = a.Core.GetRating(gCtx, activeDate)
		return nil
	})
	g.Wait() //nolint:errcheck // errors are handled per-call inside clients

	if content == "" {
		return replyToCallback(ctx, tgBot, query, "❌ Не удалось прочитать заметку.", nil)
	}

	// rating / hasRating already populated above
	ratingText := "Оценка: не установлена"
	if hasRating {
		ratingText = fmt.Sprintf("Оценка: %d", rating)
	}

	preview := content
	if len(preview) > notePreviewMaxChars {
		preview = preview[:notePreviewMaxChars] + "..."
	}

	text := fmt.Sprintf("📝 Заметка %s\n\n%s\n\n```\n%s\n```",
		activeDate,
		ratingText,
		preview,
	)
	kb := a.getMainMenuKeyboard(ctx)
	return replyToCallback(ctx, tgBot, query, text, &kb)
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
