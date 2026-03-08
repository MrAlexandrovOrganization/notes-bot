package tghandlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/clients"
	"notes_bot/frontends/telegram/tgkeyboards"
	"notes_bot/frontends/telegram/tgstates"
)

const notePreviewMaxChars = 3800

func (a *App) HandleCallback(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update) {
	query := update.CallbackQuery
	if query == nil || query.Data == "" || query.From == nil {
		return
	}

	tgBot.Request(tgbotapi.NewCallback(query.ID, ""))

	userID := query.From.ID
	if a.Cfg.RootID != 0 && userID != a.Cfg.RootID {
		replyToCallback(tgBot, query, "⛔ Unauthorized access\\.", nil)
		a.Logger.Warn("unauthorized callback", zap.Int64("user_id", userID))
		return
	}

	parts := strings.Split(query.Data, ":")
	if len(parts) == 0 {
		return
	}

	action := parts[0]

	defer func() {
		if r := recover(); r != nil {
			a.Logger.Error("panic in callback handler", zap.Any("recover", r), zap.String("data", query.Data))
		}
	}()

	var err error
	switch action {
	case "menu":
		err = a.handleMenuAction(ctx, tgBot, query, userID, parts)
	case "task":
		err = a.handleTaskAction(ctx, tgBot, query, userID, parts)
	case "cal":
		err = a.handleCalAction(ctx, tgBot, query, userID, parts)
	case "reminder":
		err = a.handleReminderAction(ctx, tgBot, query, userID, parts)
	default:
		a.Logger.Warn("unknown callback action", zap.String("action", action))
	}

	if err != nil {
		if _, ok := err.(*clients.NotificationsUnavailableError); ok {
			replyToCallback(tgBot, query, "⏳ Сервис уведомлений ещё запускается\\. Попробуйте через несколько секунд\\.", nil)
			return
		}
		a.Logger.Error("callback error", zap.String("data", query.Data), zap.Error(err))
		replyToCallback(tgBot, query, "❌ Произошла ошибка при обработке действия\\.", nil)
	}
}

// ── Menu ──────────────────────────────────────────────────────────────────

func (a *App) handleMenuAction(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	switch parts[1] {
	case "rating":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingRating })
		return replyToCallback(tgBot, query, "📊 Введите оценку дня \\(0\\-10\\):", nil)

	case "tasks":
		uc, _ := a.State.GetContext(ctx, userID)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateTasksView
			u.TaskPage = 0
		})
		a.Core.EnsureNote(ctx, uc.ActiveDate)
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
		tgBot.Request(tgbotapi.NewCallbackWithAlert(query.ID, "❌ Ошибка при переключении задачи"))

	case "add":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateWaitingNewTask })
		kb := tgkeyboards.TaskAdd()
		return replyToCallback(tgBot, query, "➕ Введите текст новой задачи:", &kb)

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
		a.State.SetActiveDate(ctx, userID, date)
		a.Core.EnsureNote(ctx, date)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })
		text := fmt.Sprintf("✅ Выбрана дата: %s\n\n📅 Активная дата: %s\n\nВыберите действие:",
			bot.EscapeMarkdownV2(date), bot.EscapeMarkdownV2(date))
		kb := tgkeyboards.MainMenu(date)
		return replyToCallback(tgBot, query, text, &kb)

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

	switch sub {
	case "create":
		a.HandleReminderCreate(ctx, tgBot, query, userID)

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
	uc, _ := a.State.GetContext(ctx, userID)
	text := fmt.Sprintf("📅 Активная дата: %s\n\nВыберите действие:", bot.EscapeMarkdownV2(uc.ActiveDate))
	kb := tgkeyboards.MainMenu(uc.ActiveDate)
	return replyToCallback(tgBot, query, text, &kb)
}

func (a *App) showTasks(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	uc, _ := a.State.GetContext(ctx, userID)
	tasks, _ := a.Core.GetTasks(ctx, uc.ActiveDate)
	kb := tgkeyboards.Tasks(tasks, uc.TaskPage)
	text := fmt.Sprintf("✅ Задачи на %s:\n\nВсего задач: %d", bot.EscapeMarkdownV2(uc.ActiveDate), len(tasks))
	if len(tasks) == 0 {
		text = fmt.Sprintf("✅ Задачи на %s:\n\nЗадач пока нет\\.", bot.EscapeMarkdownV2(uc.ActiveDate))
	}
	return replyToCallback(tgBot, query, text, &kb)
}

func (a *App) showCalendar(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	uc, _ := a.State.GetContext(ctx, userID)
	existingDatesList, _ := a.Core.GetExistingDates(ctx)
	existingDates := make(map[string]bool, len(existingDatesList))
	for _, d := range existingDatesList {
		existingDates[d] = true
	}
	kb := tgkeyboards.Calendar(uc.CalendarYear, uc.CalendarMonth, uc.ActiveDate, existingDates)
	text := fmt.Sprintf("📅 Календарь\n\nАктивная дата: %s", bot.EscapeMarkdownV2(uc.ActiveDate))
	return replyToCallback(tgBot, query, text, &kb)
}

func (a *App) showNote(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	uc, _ := a.State.GetContext(ctx, userID)
	activeDate := uc.ActiveDate
	a.Core.EnsureNote(ctx, activeDate)
	content, _ := a.Core.GetNote(ctx, activeDate)
	if content == "" {
		return replyToCallback(tgBot, query, "❌ Не удалось прочитать заметку\\.", nil)
	}

	rating, hasRating, _ := a.Core.GetRating(ctx, activeDate)
	ratingText := "Оценка: не установлена"
	if hasRating {
		ratingText = fmt.Sprintf("Оценка: %d", rating)
	}

	preview := content
	if len(preview) > notePreviewMaxChars {
		preview = preview[:notePreviewMaxChars] + "..."
	}

	text := fmt.Sprintf("📝 Заметка %s\n\n%s\n\n```\n%s\n```",
		bot.EscapeMarkdownV2(activeDate),
		bot.EscapeMarkdownV2(ratingText),
		bot.EscapeMarkdownV2(preview),
	)
	kb := tgkeyboards.MainMenu(activeDate)
	return replyToCallback(tgBot, query, text, &kb)
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
