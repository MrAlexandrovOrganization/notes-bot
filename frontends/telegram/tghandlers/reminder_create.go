package tghandlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

// scheduleTypeHandlers maps each schedule type to the handler that sets up the next
// wizard step. Types not listed (e.g. "daily") fall through to changeStateToTaskConfirm.
var scheduleTypeHandlers = map[string]func(*App, context.Context, *tgbotapi.BotAPI, *tgbotapi.CallbackQuery, int64){
	"weekly":      (*App).handleScheduleTypeWeekly,
	"monthly":     (*App).handleScheduleTypeMonthly,
	"custom_days": (*App).handleScheduleTypeCustomDays,
	"once":        (*App).handleScheduleTypeOnce,
	"yearly":      (*App).handleScheduleTypeYearly,
}

func reminderProgress(uc *tgstates.UserContext, prompt tgfmt.HTML) tgfmt.HTML {
	parts := make([]tgfmt.HTML, 0, 3)
	if uc.ReminderDraft.Title != "" {
		parts = append(parts, tgfmt.Join(tgfmt.Escape("🔔 Напоминание:\n"), tgfmt.Blockquote(tgfmt.Escape(uc.ReminderDraft.Title))))
	}
	if uc.ReminderDraft.ScheduleType != "" {
		parts = append(parts, tgfmt.Escape(fmt.Sprintf("Тип: %s", scheduleLabel(uc.ReminderDraft.ScheduleType))))
	}
	if len(parts) == 0 {
		return prompt
	}
	parts = append(parts, tgfmt.Raw("\n\n"), prompt)
	return tgfmt.Join(parts...)
}

func (a *App) HandleReminderCreate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTitle
		u.ReminderDraft = tgstates.ReminderDraft{}
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("🔔 Введите название напоминания:"), &kb)
}

func (a *App) handleReminderTitleInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateScheduleType
		u.ReminderDraft.Title = text
	})
	kb := tgkeyboards.ScheduleType()
	a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID,
		tgfmt.Join(
			tgfmt.Escape("Название: "),
			tgfmt.Blockquote(tgfmt.Escape(text)),
			tgfmt.Escape(("\n\nВыберите тип расписания:")),
		),
		&kb)
}

func (a *App) HandleReminderTypeSelect(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, scheduleType string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderDraft.ScheduleType = scheduleType
	})

	if h, ok := scheduleTypeHandlers[scheduleType]; ok {
		h(a, ctx, tgBot, query, userID)
	} else { // daily
		a.changeStateToTaskConfirm(ctx, tgBot, query, userID)
	}
}

func (a *App) handleScheduleTypeWeekly(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	cancelKb := tgkeyboards.ReminderCancel()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDay
	})
	replyToCallback(ctx, tgBot, query, reminderProgress(uc,
		tgfmt.Join(
			tgfmt.Escape("Введите дни недели через запятую (0=Пн, 1=Вт, …, 6=Вс).\nПример: "),
			tgfmt.Code(tgfmt.Escape("0,2,4")),
		)),
		&cancelKb)
}

func (a *App) handleScheduleTypeMonthly(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	cancelKb := tgkeyboards.ReminderCancel()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDay
	})
	replyToCallback(ctx, tgBot, query, reminderProgress(uc, tgfmt.Escape("Введите число месяца (1–31):")), &cancelKb)
}

func (a *App) handleScheduleTypeCustomDays(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	uc, _ := a.State.GetContext(ctx, userID)
	cancelKb := tgkeyboards.ReminderCancel()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateInterval
	})
	replyToCallback(ctx, tgBot, query, reminderProgress(uc,
		tgfmt.Join(
			tgfmt.Escape("Введите интервал в днях (например "),
			tgfmt.Code(tgfmt.Escape("3")),
			tgfmt.Escape("):"),
		)),
		&cancelKb)
}

func (a *App) handleScheduleTypeOnce(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	a.startReminderDatePicker(ctx, tgBot, query, userID, "once", tgfmt.Escape("📅 Выберите дату:"))
}

func (a *App) handleScheduleTypeYearly(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	a.startReminderDatePicker(ctx, tgBot, query, userID, "yr", tgfmt.Escape("📅 Выберите день года:"))
}

func (a *App) startReminderDatePicker(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, calCtx string, prompt tgfmt.HTML) {
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDate
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCalendar(now.Year(), int(now.Month()), calCtx, a.Cfg.TimezoneOffsetHours)
	uc, _ := a.State.GetContext(ctx, userID)
	replyToCallback(ctx, tgBot, query, reminderProgress(uc, prompt), &kb)
}

func (a *App) changeStateToTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
	})
	kb := tgkeyboards.TaskConfirm()
	uc, _ := a.State.GetContext(ctx, userID)
	replyToCallback(ctx, tgBot, query, reminderProgress(uc, tgfmt.Escape("➕ Создавать задачу в заметке при срабатывании напоминания?")), &kb)
}

func (a *App) changeStateToTaskConfirmFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
	})
	kb := tgkeyboards.TaskConfirm()
	uc, _ := a.State.GetContext(ctx, userID)
	a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, reminderProgress(uc, tgfmt.Escape("➕ Создавать задачу в заметке при срабатывании напоминания?")), &kb)
}

func (a *App) HandleReminderTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, createTask bool) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTime
		u.ReminderDraft.CreateTask = createTask
	})
	kb := tgkeyboards.ReminderCancel()
	uc, _ := a.State.GetContext(ctx, userID)
	replyToCallback(ctx, tgBot, query, reminderProgress(uc,
		tgfmt.Join(
			tgfmt.Escape("Введите время в формате "),
			tgfmt.Code(tgfmt.Escape("ЧЧ:ММ")),
			tgfmt.Escape(" (например "),
			tgfmt.Code(tgfmt.Escape("09:30")),
			tgfmt.Escape("):"),
		)),
		&kb)
}

func (a *App) handleReminderParamInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	cancelKb := tgkeyboards.ReminderCancel()

	switch uc.State {
	case tgstates.StateReminderCreateDay:
		switch uc.ReminderDraft.ScheduleType {
		case "weekly":
			var days []int
			for part := range strings.SplitSeq(text, ",") {
				d, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil || d < 0 || d > 6 {
					a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Введите числа от 0 до 6 через запятую."), &cancelKb)
					return
				}
				days = append(days, d)
			}
			a.updateState(ctx, userID, func(u *tgstates.UserContext) {
				u.ReminderDraft.Days = days
			})

		case "monthly":
			d, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || d < 1 || d > 31 {
				a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Введите число от 1 до 31."), &cancelKb)
				return
			}
			a.updateState(ctx, userID, func(u *tgstates.UserContext) {
				u.ReminderDraft.DayOfMonth = d
			})
		}
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID)

	case tgstates.StateReminderCreateInterval:
		interval, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || interval < 1 {
			a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Введите положительное целое число."), &cancelKb)
			return
		}
		a.updateState(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.IntervalDays = interval
		})
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID)

	case tgstates.StateReminderCreateTime:
		parts := strings.SplitN(strings.TrimSpace(text), ":", 2)
		if len(parts) != 2 {
			a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
			return
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
			return
		}
		a.updateState(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.Hour = h
			u.ReminderDraft.Minute = m
		})
		a.finalizeReminderFromUpdate(ctx, tgBot, update, userID)
	}
}

// finalizeReminderFromUpdate creates the reminder from the current draft.
// Returns true when the reminder was created successfully.
func (a *App) finalizeReminderFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64) bool {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return false
	}
	draft := uc.ReminderDraft

	title := draft.Title
	if title == "" {
		title = "Напоминание"
	}
	scheduleType := draft.ScheduleType
	if scheduleType == "" {
		scheduleType = "daily"
	}

	scheduleParams := draft.ToScheduleParams(a.Cfg.TimezoneOffsetHours)

	cancelKb := tgkeyboards.ReminderCancel()

	result, err := a.Notifications.CreateReminder(ctx, userID, title, scheduleType, scheduleParams, draft.CreateTask)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.InvalidArgument {
			if strings.Contains(st.Message(), "past") {
				// Only the "time already passed" case retries the time input;
				// other validation errors need different fixes.
				a.updateState(ctx, userID, func(u *tgstates.UserContext) {
					u.State = tgstates.StateReminderCreateTime
				})
				a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID,
					tgfmt.Join(
						tgfmt.Escape("❌ Выбранное время уже прошло.\nВведите другое время в формате "),
						tgfmt.Code(tgfmt.Escape("ЧЧ:ММ")),
						tgfmt.Escape(":"),
					),
					&cancelKb)
				return false
			}
			a.updateState(ctx, userID, func(u *tgstates.UserContext) {
				u.State = tgstates.StateReminderList
			})
			a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID,
				tgfmt.Join(
					tgfmt.Escape("❌ Некорректные параметры напоминания: "),
					tgfmt.Escape(st.Message()),
				), nil)
			return false
		}
		log.Error("create reminder", zap.Error(err))
		a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, tgfmt.Escape("❌ Не удалось создать напоминание."), nil)
		return false
	}

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		reminders = nil // fallback to empty list
	}
	kb := tgkeyboards.RemindersList(reminders, 0)

	var msgText tgfmt.HTML
	if result != nil {
		nextFire := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours)
		var taskNote tgfmt.HTML
		if draft.CreateTask {
			taskNote = tgfmt.Escape(" (задача будет создана)")
		}
		msgText = tgfmt.Join(
			tgfmt.Raw("✅ Напоминание создано!\n\n"),
			tgfmt.Blockquote(tgfmt.Escape(title)), taskNote, tgfmt.Raw("\n"),
			tgfmt.Escape(fmt.Sprintf("Тип: %s\nСледующее: %s", scheduleLabel(scheduleType), nextFire)),
		)
	} else {
		msgText = tgfmt.Escape("❌ Не удалось создать напоминание.")
	}
	a.replyToStateMessage(ctx, tgBot, update.Message.Chat.ID, userID, msgText, &kb)
	log.Info("created reminder", zap.Int64("user_id", userID), zap.String("title", title))
	return result != nil
}

// ── Calendar navigation ────────────────────────────────────────────────────

func (a *App) HandleReminderCalPrev(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		applog.With(ctx, a.Logger).Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	month, year := calMonthYear(uc, a.Cfg.TimezoneOffsetHours)
	month, year = stepMonth(month, year, -1)
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalNext(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		applog.With(ctx, a.Logger).Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	month, year := calMonthYear(uc, a.Cfg.TimezoneOffsetHours)
	month, year = stepMonth(month, year, 1)
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalToday(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalSelect(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, dateStr, contextName string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	cancelKb := tgkeyboards.ReminderCancel()

	if contextName == "pp" {
		a.updateState(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderPostponeTime
			u.PendingPostponeDate = dateStr
		})
		kb := tgkeyboards.ReminderCancel()
		replyToCallback(ctx, tgBot, query,
			tgfmt.Join(
				tgfmt.Escape("Дата: "), tgfmt.Code(tgfmt.Escape(dateStr)),
				tgfmt.Escape("\n\nВведите время в формате "),
				tgfmt.Code(tgfmt.Escape("ЧЧ:ММ")),
				tgfmt.Escape(":"),
			),
			&kb)
		return
	}

	if contextName == "yr" {
		dt, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Неверная дата."), &cancelKb)
			return
		}
		a.updateState(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.Month = int(dt.Month())
			u.ReminderDraft.Day = dt.Day()
		})
	} else {
		a.updateState(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.Date = dateStr
		})
	}

	a.changeStateToTaskConfirm(ctx, tgBot, query, userID)
}

func calPrompt(contextName string) tgfmt.HTML {
	if contextName == "pp" {
		return tgfmt.Escape("📅 Выберите дату переноса:")
	}
	return tgfmt.Escape("📅 Выберите дату:")
}
