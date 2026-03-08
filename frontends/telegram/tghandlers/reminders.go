package tghandlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/clients"
	"notes_bot/frontends/telegram/tgkeyboards"
	"notes_bot/frontends/telegram/tgstates"
)

func scheduleLabel(scheduleType string) string {
	labels := map[string]string{
		"daily":       "каждый день",
		"weekly":      "по дням недели",
		"monthly":     "каждый месяц",
		"yearly":      "каждый год",
		"once":        "один раз",
		"custom_days": "каждые N дней",
	}
	if l, ok := labels[scheduleType]; ok {
		return l
	}
	return scheduleType
}

func formatLocalTime(utcStr string, tzOffsetHours int) string {
	if utcStr == "" {
		return "—"
	}
	s := strings.ReplaceAll(utcStr, "Z", "+00:00")
	dt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if len(utcStr) >= 16 {
			return utcStr[:16]
		}
		return utcStr
	}
	loc := time.FixedZone("local", tzOffsetHours*3600)
	return dt.In(loc).Format("02.01.2006 15:04")
}

func reminderListText(reminders []*clients.ReminderInfo, page, tzOffset int) string {
	if len(reminders) == 0 {
		return "🔔 Уведомления:\n\nНапоминаний пока нет\\."
	}
	perPage := 5
	start := page * perPage
	end := start + perPage
	if end > len(reminders) {
		end = len(reminders)
	}
	var lines []string
	for _, r := range reminders[start:end] {
		lines = append(lines, fmt.Sprintf("• %s \\(%s\\) — %s",
			bot.EscapeMarkdownV2(r.Title),
			bot.EscapeMarkdownV2(scheduleLabel(r.ScheduleType)),
			bot.EscapeMarkdownV2(formatLocalTime(r.NextFireAt, tzOffset)),
		))
	}
	return "🔔 Уведомления:\n\n" + strings.Join(lines, "\n")
}

func localNow(tzOffset int) time.Time {
	return time.Now().UTC().Add(time.Duration(tzOffset) * time.Hour)
}

func calMonthYear(uc *tgstates.UserContext, tzOffset int) (int, int) {
	now := localNow(tzOffset)
	month := uc.ReminderCalMonth
	year := uc.ReminderCalYear
	if month == 0 {
		month = int(now.Month())
	}
	if year == 0 {
		year = now.Year()
	}
	return month, year
}

// ── List & Navigation ──────────────────────────────────────────────────────

func (a *App) HandleMenuNotifications(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})

	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		if _, ok := err.(*clients.NotificationsUnavailableError); ok {
			replyToCallback(tgBot, query, "⏳ Сервис уведомлений ещё запускается\\. Попробуйте через несколько секунд\\.", nil)
			return
		}
		a.Logger.Error("list reminders", zap.Error(err))
		return
	}

	page := uc.ReminderListPage
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
	a.Logger.Info("user opened reminders", zap.Int64("user_id", userID))
}

func (a *App) HandleReminderPage(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, page int) {
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderListPage = page
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		return
	}
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
}

// ── Create wizard ──────────────────────────────────────────────────────────

func (a *App) HandleReminderCreate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	now := localNow(a.Cfg.TimezoneOffsetHours)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTitle
		u.ReminderDraft = map[string]any{}
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(tgBot, query, "🔔 Введите название напоминания:", &kb)
}

func (a *App) handleReminderTitleInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	draft["title"] = text
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateScheduleType
		u.ReminderDraft = draft
	})
	kb := tgkeyboards.ScheduleType()
	replyToUpdate(tgBot, update,
		fmt.Sprintf("Название: *%s*\n\nВыберите тип расписания:", bot.EscapeMarkdownV2(text)),
		&kb)
}

func (a *App) HandleReminderTypeSelect(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, scheduleType string) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	draft["schedule_type"] = scheduleType
	cancelKb := tgkeyboards.ReminderCancel()
	now := localNow(a.Cfg.TimezoneOffsetHours)

	switch scheduleType {
	case "weekly":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderCreateDay
			u.ReminderDraft = draft
		})
		replyToCallback(tgBot, query,
			"Введите дни недели через запятую \\(0\\=Пн, 1\\=Вт, …, 6\\=Вс\\)\\.\nПример: `0,2,4`",
			&cancelKb)

	case "monthly":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderCreateDay
			u.ReminderDraft = draft
		})
		replyToCallback(tgBot, query, "Введите число месяца \\(1–31\\):", &cancelKb)

	case "custom_days":
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderCreateInterval
			u.ReminderDraft = draft
		})
		replyToCallback(tgBot, query, "Введите интервал в днях \\(например `3`\\):", &cancelKb)

	case "once", "yearly":
		ctxName := "once"
		promptText := "📅 Выберите дату:"
		if scheduleType == "yearly" {
			ctxName = "yr"
			promptText = "📅 Выберите день года:"
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderCreateDate
			u.ReminderDraft = draft
			u.ReminderCalMonth = int(now.Month())
			u.ReminderCalYear = now.Year()
		})
		kb := tgkeyboards.ReminderCalendar(now.Year(), int(now.Month()), ctxName, a.Cfg.TimezoneOffsetHours)
		replyToCallback(tgBot, query, promptText, &kb)

	default: // daily
		a.changeStateToTaskConfirm(ctx, tgBot, query, userID, draft)
	}
}

func (a *App) changeStateToTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, draft map[string]any) {
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
		u.ReminderDraft = draft
	})
	kb := tgkeyboards.TaskConfirm()
	replyToCallback(tgBot, query, "➕ Создавать задачу в заметке при срабатывании напоминания?", &kb)
}

func (a *App) changeStateToTaskConfirmFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, draft map[string]any) {
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
		u.ReminderDraft = draft
	})
	kb := tgkeyboards.TaskConfirm()
	replyToUpdate(tgBot, update, "➕ Создавать задачу в заметке при срабатывании напоминания?", &kb)
}

func (a *App) HandleReminderTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, createTask bool) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	draft["create_task"] = createTask

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTime
		u.ReminderDraft = draft
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(tgBot, query, "Введите время в формате `ЧЧ:ММ` \\(например `09:30`\\):", &kb)
}

func (a *App) handleReminderParamInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	scheduleType := draftStr(draft, "schedule_type")
	cancelKb := tgkeyboards.ReminderCancel()

	switch uc.State {
	case tgstates.StateReminderCreateDay:
		switch scheduleType {
		case "weekly":
			parts := strings.Split(text, ",")
			var days []int
			valid := true
			for _, p := range parts {
				d, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || d < 0 || d > 6 {
					valid = false
					break
				}
				days = append(days, d)
			}
			if !valid {
				replyToUpdate(tgBot, update, "❌ Введите числа от 0 до 6 через запятую\\.", &cancelKb)
				return
			}
			draft["days"] = days

		case "monthly":
			d, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || d < 1 || d > 31 {
				replyToUpdate(tgBot, update, "❌ Введите число от 1 до 31\\.", &cancelKb)
				return
			}
			draft["day_of_month"] = d
		}
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID, draft)

	case tgstates.StateReminderCreateInterval:
		interval, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || interval < 1 {
			replyToUpdate(tgBot, update, "❌ Введите положительное целое число\\.", &cancelKb)
			return
		}
		draft["interval_days"] = interval
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID, draft)

	case tgstates.StateReminderCreateTime:
		parts := strings.Split(strings.TrimSpace(text), ":")
		if len(parts) != 2 {
			replyToUpdate(tgBot, update, "❌ Введите время в формате ЧЧ:ММ\\.", &cancelKb)
			return
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			replyToUpdate(tgBot, update, "❌ Введите время в формате ЧЧ:ММ\\.", &cancelKb)
			return
		}
		draft["hour"] = h
		draft["minute"] = m
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft = draft
		})
		a.finalizeReminderFromUpdate(ctx, tgBot, update, userID)
	}
}

func (a *App) finalizeReminderFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	title := draftStr(draft, "title")
	if title == "" {
		title = "Напоминание"
	}
	scheduleType := draftStr(draft, "schedule_type")
	if scheduleType == "" {
		scheduleType = "daily"
	}
	createTask := draftBool(draft, "create_task")
	delete(draft, "title")
	delete(draft, "schedule_type")
	delete(draft, "create_task")
	draft["tz_offset"] = a.Cfg.TimezoneOffsetHours

	paramsJSON, _ := json.Marshal(draft)
	cancelKb := tgkeyboards.ReminderCancel()

	result, err := a.Notifications.CreateReminder(ctx, userID, title, scheduleType, string(paramsJSON), createTask)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.InvalidArgument {
			a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
				u.State = tgstates.StateReminderCreateTime
			})
			replyToUpdate(tgBot, update,
				"❌ Выбранное время уже прошло\\.\nВведите другое время в формате `ЧЧ:ММ`:",
				&cancelKb)
			return
		}
		a.Logger.Error("create reminder", zap.Error(err))
		replyToUpdate(tgBot, update, "❌ Не удалось создать напоминание\\.", nil)
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
	kb := tgkeyboards.RemindersList(reminders, 0)

	var msgText string
	if result != nil {
		nextFire := formatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours)
		taskNote := ""
		if createTask {
			taskNote = " \\(задача будет создана\\)"
		}
		msgText = fmt.Sprintf("✅ Напоминание создано\\!\n\n*%s*%s\nТип: %s\nСледующее: %s",
			bot.EscapeMarkdownV2(title), taskNote,
			bot.EscapeMarkdownV2(scheduleLabel(scheduleType)),
			bot.EscapeMarkdownV2(nextFire))
	} else {
		msgText = "❌ Не удалось создать напоминание\\."
	}
	replyToUpdate(tgBot, update, msgText, &kb)
	a.Logger.Info("created reminder", zap.Int64("user_id", userID), zap.String("title", title))
}

// ── Calendar navigation ────────────────────────────────────────────────────

func (a *App) HandleReminderCalPrev(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	uc, _ := a.State.GetContext(ctx, userID)
	month, year := calMonthYear(uc, a.Cfg.TimezoneOffsetHours)
	if month == 1 {
		month, year = 12, year-1
	} else {
		month--
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalNext(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	uc, _ := a.State.GetContext(ctx, userID)
	month, year := calMonthYear(uc, a.Cfg.TimezoneOffsetHours)
	if month == 12 {
		month, year = 1, year+1
	} else {
		month++
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalToday(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	now := localNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalSelect(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, dateStr, contextName string) {
	uc, _ := a.State.GetContext(ctx, userID)
	draft := copyDraft(uc.ReminderDraft)
	cancelKb := tgkeyboards.ReminderCancel()

	if contextName == "pp" {
		reminderID := uc.PendingPostponeReminderID
		if reminderID != 0 {
			a.Notifications.PostponeReminder(ctx, reminderID, userID, 0, dateStr, 0)
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateIdle
			u.PendingPostponeReminderID = 0
		})
		replyToCallback(tgBot, query, fmt.Sprintf("✅ Напоминание перенесено на %s\\.", bot.EscapeMarkdownV2(dateStr)), nil)
		return
	}

	if contextName == "yr" {
		dt, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			replyToCallback(tgBot, query, "❌ Неверная дата\\.", &cancelKb)
			return
		}
		draft["month"] = int(dt.Month())
		draft["day"] = dt.Day()
	} else {
		draft["date"] = dateStr
	}

	a.changeStateToTaskConfirm(ctx, tgBot, query, userID, draft)
}

func calPrompt(contextName string) string {
	if contextName == "pp" {
		return "📅 Выберите дату переноса:"
	}
	return "📅 Выберите дату:"
}

// ── Delete, Done, Postpone, Back ───────────────────────────────────────────

func (a *App) HandleReminderDelete(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	a.Notifications.DeleteReminder(ctx, reminderID, userID)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
	kb := tgkeyboards.RemindersList(reminders, 0)
	text := "🔔 Уведомления:\n\nНапоминание удалено\\."
	if len(reminders) > 0 {
		text = "🔔 Уведомления:"
	}
	replyToCallback(tgBot, query, text, &kb)
	a.Logger.Info("deleted reminder", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderDone(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64, createTaskFlag int, dateStr string) {
	if createTaskFlag == 1 && dateStr != "" && query.Message != nil {
		msgText := query.Message.Text
		title := strings.TrimPrefix(msgText, "🔔 Напоминание: ")
		tasks, err := a.Core.GetTasks(ctx, dateStr)
		if err == nil {
			for _, t := range tasks {
				if t.Text == title && !t.Completed {
					a.Core.ToggleTask(ctx, dateStr, t.Index)
					break
				}
			}
		}
	}

	original := ""
	if query.Message != nil {
		original = bot.EscapeMarkdownV2(query.Message.Text)
	}
	replyToCallback(tgBot, query, original+"\n\n✅ _Принято\\!_", nil)
	a.Logger.Info("reminder acknowledged", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderPostponeDays(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, days, reminderID int64) {
	result, _ := a.Notifications.PostponeReminder(ctx, reminderID, userID, int32(days), "", 0)
	a.sendPostponeResult(tgBot, query, result, days, "д", userID, reminderID)
}

func (a *App) HandleReminderPostponeHours(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, hours, reminderID int64) {
	result, _ := a.Notifications.PostponeReminder(ctx, reminderID, userID, 0, "", int32(hours))
	a.sendPostponeResult(tgBot, query, result, hours, "ч", userID, reminderID)
}

func (a *App) sendPostponeResult(tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, result *clients.ReminderInfo, amount int64, unit string, userID, reminderID int64) {
	nextFireText := ""
	if result != nil {
		nextFire := formatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours)
		if nextFire != "" {
			nextFireText = fmt.Sprintf(" \\(следующее: %s\\)", bot.EscapeMarkdownV2(nextFire))
		}
	}
	original := ""
	if query.Message != nil {
		original = bot.EscapeMarkdownV2(query.Message.Text)
	}
	text := fmt.Sprintf("%s\n\n⏰ _Перенесено на %d %s\\._", original, amount, unit) + nextFireText
	replyToCallback(tgBot, query, text, nil)
	a.Logger.Info("reminder postponed", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderCustomDate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	now := localNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderPostponeDate
		u.PendingPostponeReminderID = reminderID
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, "pp", a.Cfg.TimezoneOffsetHours)
	replyToCallback(tgBot, query, "📅 Выберите дату переноса:", &kb)
}

func (a *App) HandleReminderBack(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	activeDate := uc.ActiveDate
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.ReminderDraft = map[string]any{}
		u.PendingPostponeReminderID = 0
	})
	kb := tgkeyboards.MainMenu(activeDate)
	replyToCallback(tgBot, query,
		fmt.Sprintf("📅 Активная дата: %s\n\nВыберите действие:", bot.EscapeMarkdownV2(activeDate)),
		&kb)
}

func (a *App) HandleReminderCancel(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
	kb := tgkeyboards.RemindersList(reminders, 0)
	text := "🔔 Уведомления:"
	if len(reminders) == 0 {
		text = "🔔 Уведомления:\n\nНапоминаний пока нет\\."
	}
	replyToCallback(tgBot, query, text, &kb)
}

// ── Helpers ───────────────────────────────────────────────────────────────

func copyDraft(d map[string]any) map[string]any {
	if d == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

func draftStr(d map[string]any, key string) string {
	v, ok := d[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func draftBool(d map[string]any, key string) bool {
	v, ok := d[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
