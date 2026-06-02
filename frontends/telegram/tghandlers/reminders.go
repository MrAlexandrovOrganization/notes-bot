package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

var scheduleLabels = map[string]string{
	"daily":       "каждый день",
	"weekly":      "по дням недели",
	"monthly":     "каждый месяц",
	"yearly":      "каждый год",
	"once":        "один раз",
	"custom_days": "каждые N дней",
}

func scheduleLabel(scheduleType string) string {
	if l, ok := scheduleLabels[scheduleType]; ok {
		return l
	}
	return scheduleType
}

func reminderListText(reminders []*clients.ReminderInfo, page, tzOffset int) tgfmt.HTML {
	header := tgfmt.Raw("🔔 Уведомления:\n\n")
	amountReminders := len(reminders)
	if amountReminders == 0 {
		return header + tgfmt.Raw("Напоминаний пока нет.")
	}
	perPage := 5
	page = min((amountReminders-1)/perPage, page)

	start := page * perPage
	end := min(start+perPage, amountReminders)
	lines := make([]tgfmt.HTML, 0, end-start)
	for _, r := range reminders[start:end] {
		lines = append(lines, tgfmt.Join(
			tgfmt.Escape("• "),
			tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", r.Title))),
			tgfmt.Escape(fmt.Sprintf(" (%s) — %s",
				scheduleLabel(r.ScheduleType),
				timeutil.FormatLocalTime(r.NextFireAt, tzOffset),
			))))
	}
	parts := make([]tgfmt.HTML, 0, len(lines)*2)
	for i, l := range lines {
		parts = append(parts, l)
		if i < len(lines)-1 {
			parts = append(parts, tgfmt.Raw("\n"))
		}
	}
	return header + tgfmt.Join(parts...)
}

func calMonthYear(uc *tgstates.UserContext, tzOffset int) (int, int) {
	now := timeutil.LocalNow(tzOffset)
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

var dayNamesRu = []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

var monthNamesRu = map[int]string{
	1: "января", 2: "февраля", 3: "марта", 4: "апреля",
	5: "мая", 6: "июня", 7: "июля", 8: "августа",
	9: "сентября", 10: "октября", 11: "ноября", 12: "декабря",
}

func pluralDays(n int) string {
	mod10, mod100 := n%10, n%100
	switch {
	case mod10 == 1 && mod100 != 11:
		return "день"
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20):
		return "дня"
	default:
		return "дней"
	}
}

// formatNLReminderPreview builds a human-readable summary of the LLM-parsed reminder.
func formatNLReminderPreview(r *clients.LLMReminderResult) tgfmt.HTML {
	schedule := scheduleLabel(r.ScheduleType)
	switch r.ScheduleType {
	case "weekly":
		var daysStr []string
		for _, d := range r.Days {
			if d >= 0 && d <= 6 {
				daysStr = append(daysStr, dayNamesRu[d])
			}
		}
		if len(daysStr) > 0 {
			schedule = "по " + strings.Join(daysStr, ", ")
		}
	case "monthly":
		schedule = fmt.Sprintf("каждый месяц %d числа", r.DayOfMonth)
	case "yearly":
		schedule = fmt.Sprintf("каждый год %d.%02d", r.Day, r.Month)
	case "once":
		if t, err := time.Parse("2006-01-02", r.Date); err == nil {
			schedule = fmt.Sprintf("один раз, %d %s %d", t.Day(), monthNamesRu[int(t.Month())], t.Year())
		} else if r.Date != "" {
			schedule = fmt.Sprintf("один раз, %s", r.Date)
		} else {
			schedule = "один раз, ⚠️ дата не распознана"
		}
	case "custom_days":
		schedule = fmt.Sprintf("каждые %d %s", r.IntervalDays, pluralDays(r.IntervalDays))
	}

	taskNote := "нет"
	if r.CreateTask {
		taskNote = "да"
	}

	return tgfmt.Escape(fmt.Sprintf(
		"🧠 Я понял так:\n\n📌 Название: %s\n🔄 Расписание: %s\n⏰ Время: %02d:%02d\n📋 Создавать задачу: %s",
		r.Title, schedule, r.Hour, r.Minute, taskNote,
	))
}

// ── List & Navigation ──────────────────────────────────────────────────────

func (a *App) HandleMenuNotifications(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})

	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		var svcErr *clients.ServiceUnavailableError
		if errors.As(err, &svcErr) {
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("⏳ Сервис уведомлений ещё запускается. Попробуйте через несколько секунд."), nil)
			return
		}
		log.Error("list reminders", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке напоминаний."), nil)
		return
	}

	page := uc.ReminderListPage
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(ctx, tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
	log.Info("user opened reminders", zap.Int64("user_id", userID))
}

func (a *App) HandleReminderPage(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, page int) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderListPage = page
	})
	reminders, err := a.Notifications.ListReminders(ctx, userID)
	if err != nil {
		log.Error("list reminders", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при загрузке напоминаний."), nil)
		return
	}
	kb := tgkeyboards.RemindersList(reminders, page)
	replyToCallback(ctx, tgBot, query, reminderListText(reminders, page, a.Cfg.TimezoneOffsetHours), &kb)
}

// ── Create wizard ──────────────────────────────────────────────────────────

func (a *App) HandleReminderCreate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateScheduleType
		u.ReminderDraft.Title = text
	})
	kb := tgkeyboards.ScheduleType()
	replyToUpdate(ctx, tgBot, update,
		tgfmt.Join(
			tgfmt.Escape("Название: "),
			tgfmt.Blockquote(tgfmt.Escape(fmt.Sprintf("%s", text))),
			tgfmt.Escape(("\n\nВыберите тип расписания:")),
		),
		&kb)
}

// scheduleTypeHandlers maps each schedule type to the handler that sets up the next
// wizard step. Types not listed (e.g. "daily") fall through to changeStateToTaskConfirm.
var scheduleTypeHandlers = map[string]func(*App, context.Context, *tgbotapi.BotAPI, *tgbotapi.CallbackQuery, int64){
	"weekly":      (*App).handleScheduleTypeWeekly,
	"monthly":     (*App).handleScheduleTypeMonthly,
	"custom_days": (*App).handleScheduleTypeCustomDays,
	"once":        (*App).handleScheduleTypeOnce,
	"yearly":      (*App).handleScheduleTypeYearly,
}

func (a *App) HandleReminderTypeSelect(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, scheduleType string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderDraft.ScheduleType = scheduleType
	})

	if h, ok := scheduleTypeHandlers[scheduleType]; ok {
		h(a, ctx, tgBot, query, userID)
	} else { // daily
		a.changeStateToTaskConfirm(ctx, tgBot, query, userID)
	}
}

func (a *App) handleScheduleTypeWeekly(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	cancelKb := tgkeyboards.ReminderCancel()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDay
	})
	replyToCallback(ctx, tgBot, query,
		tgfmt.Join(
			tgfmt.Escape("Введите дни недели через запятую (0=Пн, 1=Вт, …, 6=Вс).\nПример: "),
			tgfmt.Code(tgfmt.Escape("0,2,4")),
		),
		&cancelKb)
}

func (a *App) handleScheduleTypeMonthly(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	cancelKb := tgkeyboards.ReminderCancel()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDay
	})
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("Введите число месяца (1–31):"), &cancelKb)
}

func (a *App) handleScheduleTypeCustomDays(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	cancelKb := tgkeyboards.ReminderCancel()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateInterval
	})
	replyToCallback(ctx, tgBot, query,
		tgfmt.Join(
			tgfmt.Escape("Введите интервал в днях (например "),
			tgfmt.Code(tgfmt.Escape("3")),
			tgfmt.Escape("):"),
		),
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
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateDate
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCalendar(now.Year(), int(now.Month()), calCtx, a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, prompt, &kb)
}

func (a *App) changeStateToTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
	})
	kb := tgkeyboards.TaskConfirm()
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("➕ Создавать задачу в заметке при срабатывании напоминания?"), &kb)
}

func (a *App) changeStateToTaskConfirmFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTaskConfirm
	})
	kb := tgkeyboards.TaskConfirm()
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape("➕ Создавать задачу в заметке при срабатывании напоминания?"), &kb)
}

func (a *App) HandleReminderTaskConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, createTask bool) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateTime
		u.ReminderDraft.CreateTask = createTask
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query,
		tgfmt.Join(
			tgfmt.Escape("Введите время в формате "),
			tgfmt.Code(tgfmt.Escape("ЧЧ:ММ")),
			tgfmt.Escape(" (например "),
			tgfmt.Code(tgfmt.Escape("09:30")),
			tgfmt.Escape("):"),
		),
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
					replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите числа от 0 до 6 через запятую."), &cancelKb)
					return
				}
				days = append(days, d)
			}
			a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
				u.ReminderDraft.Days = days
			})

		case "monthly":
			d, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil || d < 1 || d > 31 {
				replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите число от 1 до 31."), &cancelKb)
				return
			}
			a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
				u.ReminderDraft.DayOfMonth = d
			})
		}
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID)

	case tgstates.StateReminderCreateInterval:
		interval, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || interval < 1 {
			replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите положительное целое число."), &cancelKb)
			return
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.IntervalDays = interval
		})
		a.changeStateToTaskConfirmFromUpdate(ctx, tgBot, update, userID)

	case tgstates.StateReminderCreateTime:
		parts := strings.SplitN(strings.TrimSpace(text), ":", 2)
		if len(parts) != 2 {
			replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
			return
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
			return
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.Hour = h
			u.ReminderDraft.Minute = m
		})
		a.finalizeReminderFromUpdate(ctx, tgBot, update, userID)
	}
}

func (a *App) finalizeReminderFromUpdate(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
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

	paramsJSON, err := draft.ToParamsJSON(a.Cfg.TimezoneOffsetHours)
	if err != nil {
		log.Error("marshal reminder params", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Не удалось создать напоминание."), nil)
		return
	}

	cancelKb := tgkeyboards.ReminderCancel()

	result, err := a.Notifications.CreateReminder(ctx, userID, title, scheduleType, paramsJSON, draft.CreateTask)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.InvalidArgument {
			a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
				u.State = tgstates.StateReminderCreateTime
			})
			replyToUpdate(ctx, tgBot, update,
				tgfmt.Join(
					tgfmt.Escape("❌ Выбранное время уже прошло.\nВведите другое время в формате "),
					tgfmt.Code(tgfmt.Escape("ЧЧ:ММ")),
					tgfmt.Escape(":"),
				),
				&cancelKb)
			return
		}
		log.Error("create reminder", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Не удалось создать напоминание."), nil)
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
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
	replyToUpdate(ctx, tgBot, update, msgText, &kb)
	log.Info("created reminder", zap.Int64("user_id", userID), zap.String("title", title))
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
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, contextName, a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, calPrompt(contextName), &kb)
}

func (a *App) HandleReminderCalToday(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, contextName string) {
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.ReminderDraft.Month = int(dt.Month())
			u.ReminderDraft.Day = dt.Day()
		})
	} else {
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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

// ── Delete, Done, Postpone, Back ───────────────────────────────────────────

func (a *App) HandleReminderDelete(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	if _, err := a.Notifications.DeleteReminder(ctx, reminderID, userID); err != nil {
		log.Error("delete reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при удалении напоминания."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
	kb := tgkeyboards.RemindersList(reminders, 0)
	var text tgfmt.HTML
	if len(reminders) > 0 {
		text = tgfmt.Raw("Напоминание удалено.\n\n") + reminderListText(reminders, 0, a.Cfg.TimezoneOffsetHours)
	} else {
		text = tgfmt.Raw("Напоминание удалено.\n\n🔔 Уведомления:\n\nНапоминаний пока нет.")
	}
	replyToCallback(ctx, tgBot, query, text, &kb)
	log.Info("deleted reminder", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) getMainMenuKeyboard(ctx context.Context) tgbotapi.InlineKeyboardMarkup {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	return tgkeyboards.MainMenu("")
}

func (a *App) HandleReminderDone(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64, createTaskFlag int, dateStr string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
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
		original = query.Message.Text
	}

	kb := a.getMainMenuKeyboard(ctx)

	replyToCallback(ctx, tgBot, query, tgfmt.Escape(original+"\n\n✅ Принято!"), &kb)
	log.Info("reminder acknowledged", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

// minutesToLabel converts a minute count to a human-readable Russian label.
// Mixed durations are rendered as "1 д. 3 ч.", "2 ч. 30 мин.", etc.
func minutesToLabel(n int) string {
	const month = 30 * 24 * 60
	const week = 7 * 24 * 60
	const day = 24 * 60
	const hour = 60

	var parts []string
	if n >= month {
		parts = append(parts, fmt.Sprintf("%d мес.", n/month))
		n %= month
	}
	if n >= week {
		parts = append(parts, fmt.Sprintf("%d нед.", n/week))
		n %= week
	}
	if n >= day {
		parts = append(parts, fmt.Sprintf("%d д.", n/day))
		n %= day
	}
	if n >= hour {
		parts = append(parts, fmt.Sprintf("%d ч.", n/hour))
		n %= hour
	}
	if n > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d мин.", n))
	}
	return strings.Join(parts, " ")
}

// parseDuration parses a human-readable duration string into total minutes.
//
// Supported units (case-sensitive):
//
//	m — минуты   h — часы   d — дни   w — недели   M — месяцы (≈ 30 дней)
//
// Formats:
//
//	30m  2h30m  1d12h  1w  1M  3d6h30m  (spaces between tokens are OK)
//
// A bare integer is accepted as minutes for backward compatibility.
//
// Returns an informative error with a suggested canonical form when a unit
// value overflows into the next unit (e.g. 27h → error suggesting "1d3h").
func parseDuration(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Bare integer → minutes (backward compat)
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("введите положительное значение")
		}
		return n, nil
	}

	// Remove spaces so "1d 3h 30m" → "1d3h30m"
	s = strings.ReplaceAll(s, " ", "")

	vals := make(map[byte]int)
	i := 0
	for i < len(s) {
		// Read run of digits
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return 0, fmt.Errorf("неверный формат — ожидается число перед единицей. Примеры: 30m, 2h30m, 1d12h")
		}
		if j >= len(s) {
			return 0, fmt.Errorf("неверный формат — укажите единицу после числа. Доступные: m h d w M")
		}
		n, _ := strconv.Atoi(s[i:j])
		unit := s[j]
		switch unit {
		case 'm', 'h', 'd', 'w', 'M':
		default:
			return 0, fmt.Errorf("неизвестная единица %q. Доступные: m (минуты), h (часы), d (дни), w (недели), M (месяцы)", string(unit))
		}
		if _, exists := vals[unit]; exists {
			return 0, fmt.Errorf("единица %q указана дважды", string(unit))
		}
		vals[unit] = n
		i = j + 1
	}

	if len(vals) == 0 {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Validate: each unit must be within its canonical range.
	if m, ok := vals['m']; ok && m >= 60 {
		sugg := durationSuggestion(vals, 'm')
		return 0, fmt.Errorf("%dm — это %s; введите: %s", m, durationOverflowDesc(m, 'm'), sugg)
	}
	if h, ok := vals['h']; ok && h >= 24 {
		sugg := durationSuggestion(vals, 'h')
		return 0, fmt.Errorf("%dh — это %s; введите: %s", h, durationOverflowDesc(h, 'h'), sugg)
	}
	if d, ok := vals['d']; ok && d >= 7 {
		sugg := durationSuggestion(vals, 'd')
		return 0, fmt.Errorf("%dd — это %s; введите: %s", d, durationOverflowDesc(d, 'd'), sugg)
	}

	total := vals['m'] + vals['h']*60 + vals['d']*1440 + vals['w']*10080 + vals['M']*43200
	if total <= 0 {
		return 0, fmt.Errorf("введите положительное значение")
	}
	return total, nil
}

// durationOverflowDesc returns a human-readable description of what an
// overflowing unit value actually equals. E.g. 27 hours → "1д 3ч".
func durationOverflowDesc(val int, unit byte) string {
	switch unit {
	case 'm':
		h, m := val/60, val%60
		if m > 0 {
			return fmt.Sprintf("%dч %dм", h, m)
		}
		return fmt.Sprintf("%dч", h)
	case 'h':
		d, h := val/24, val%24
		if h > 0 {
			return fmt.Sprintf("%dд %dч", d, h)
		}
		return fmt.Sprintf("%dд", d)
	case 'd':
		w, d := val/7, val%7
		if d > 0 {
			return fmt.Sprintf("%dн %dд", w, d)
		}
		return fmt.Sprintf("%dн", w)
	}
	return fmt.Sprintf("%d", val)
}

// durationSuggestion builds a canonical duration string by normalising the
// overflowing unit and carrying into higher units.
func durationSuggestion(vals map[byte]int, overflowUnit byte) string {
	nv := make(map[byte]int, len(vals))
	for k, v := range vals {
		nv[k] = v
	}

	switch overflowUnit {
	case 'm':
		nv['h'] += nv['m'] / 60
		nv['m'] = nv['m'] % 60
		fallthrough // h might now overflow too
	case 'h':
		if overflowUnit == 'h' || nv['h'] >= 24 {
			nv['d'] += nv['h'] / 24
			nv['h'] = nv['h'] % 24
		}
		fallthrough
	case 'd':
		if overflowUnit == 'd' || nv['d'] >= 7 {
			nv['w'] += nv['d'] / 7
			nv['d'] = nv['d'] % 7
		}
	}

	var parts []string
	for _, u := range []byte{'M', 'w', 'd', 'h', 'm'} {
		if v := nv[u]; v > 0 {
			parts = append(parts, fmt.Sprintf("%d%c", v, u))
		}
	}
	if len(parts) == 0 {
		return "0m"
	}
	return strings.Join(parts, "")
}

// postponeWithMinutes calls PostponeReminder and shows the result as a callback edit.
func (a *App) postponeWithMinutes(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID, reminderID int64, minutes int32, label string) {
	log := applog.With(ctx, a.Logger)
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, minutes)
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}
	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape(fmt.Sprintf("%s\n\n⏰ Перенесено на %s.", original, label)+nextFireText), &kb)
	log.Info("reminder postponed", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

// HandleReminderReject dismisses the current reminder firing without affecting the schedule.
func (a *App) HandleReminderReject(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape(original+"\n\n❌ Отклонено."), &kb) // TODO: display reminder as Block quote
	applog.With(ctx, a.Logger).Info("reminder rejected", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

// HandleReminderPostponeInput handles "⏰ Перенести" — asks user to enter a duration.
func (a *App) HandleReminderPostponeInput(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderPostponeInput
		u.PendingPostponeReminderID = reminderID
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query,
		tgfmt.Join(
			tgfmt.Escape("⏰ На сколько перенести?\n\nПоддерживаемые единицы: "),
			tgfmt.Code(tgfmt.Escape("m")), tgfmt.Escape(" мин · "),
			tgfmt.Code(tgfmt.Escape("h")), tgfmt.Escape(" ч · "),
			tgfmt.Code(tgfmt.Escape("d")), tgfmt.Escape(" дни · "),
			tgfmt.Code(tgfmt.Escape("w")), tgfmt.Escape(" недели · "),
			tgfmt.Code(tgfmt.Escape("M")), tgfmt.Escape(" месяцы\n\nПримеры: "),
			tgfmt.Code(tgfmt.Escape("30m")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("2h30m")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1d12h")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1w")),
			tgfmt.Escape(", "),
			tgfmt.Code(tgfmt.Escape("1M")),
			tgfmt.Escape("\nИли просто число минут: "),
			tgfmt.Code(tgfmt.Escape("90")),
			tgfmt.Escape(":"),
		),
		&kb)
}

// handleReminderPostponeTextInput parses a duration string and postpones the reminder.
// Accepts formats like 30m, 2h30m, 1d12h, 1w, 1M, or a plain integer (minutes).
func (a *App) handleReminderPostponeTextInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	cancelKb := tgkeyboards.ReminderCancel()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	reminderID := uc.PendingPostponeReminderID

	n, parseErr := parseDuration(text)
	if parseErr != nil {
		replyToUpdate(ctx, tgBot, update,
			tgfmt.Join(tgfmt.Escape("❌ "+parseErr.Error())),
			&cancelKb)
		return
	}

	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, int32(n))
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.PendingPostponeReminderID = 0
	})

	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	kb := a.getMainMenuKeyboard(ctx)
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape(fmt.Sprintf("⏰ Перенесено на %s.", minutesToLabel(n))+nextFireText), &kb)
	log.Info("reminder postponed via text", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID), zap.Int("minutes", n))
}

// HandleReminderPostponeDate handles "📅 На дату" — opens calendar for date selection.
func (a *App) HandleReminderPostponeDate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	month, year := int(now.Month()), now.Year()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderPostponeDate
		u.PendingPostponeReminderID = reminderID
		u.ReminderCalMonth = month
		u.ReminderCalYear = year
	})
	kb := tgkeyboards.ReminderCalendar(year, month, "pp", a.Cfg.TimezoneOffsetHours)
	replyToCallback(ctx, tgBot, query, tgfmt.Escape("📅 Выберите дату переноса:"), &kb)
}

// handleReminderPostponeTimeInput parses HH:MM, computes minutes to the pending date+time,
// and calls PostponeReminder.
func (a *App) handleReminderPostponeTimeInput(ctx context.Context, tgBot *tgbotapi.BotAPI, update *tgbotapi.Update, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)
	cancelKb := tgkeyboards.ReminderCancel()

	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		log.Error("get context", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}

	parts := strings.SplitN(strings.TrimSpace(text), ":", 2)
	if len(parts) != 2 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
		return
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Введите время в формате ЧЧ:ММ."), &cancelKb)
		return
	}

	loc := time.FixedZone("tz", a.Cfg.TimezoneOffsetHours*3600)
	d, err := time.ParseInLocation("2006-01-02", uc.PendingPostponeDate, loc)
	if err != nil {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Дата потеряна. Выберите дату заново."), nil)
		return
	}
	target := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
	minutesUntil := int32(time.Until(target).Minutes())
	if minutesUntil < 1 {
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Выбранное время уже прошло. Введите другое время:"), &cancelKb)
		return
	}

	reminderID := uc.PendingPostponeReminderID
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, minutesUntil)
	if err != nil {
		log.Error("postpone reminder", zap.Error(err))
		replyToUpdate(ctx, tgBot, update, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.PendingPostponeReminderID = 0
		u.PendingPostponeDate = ""
	})

	nextFireText := ""
	if result != nil {
		if nf := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours); nf != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nf)
		}
	}
	label := fmt.Sprintf("%s %02d:%02d", uc.PendingPostponeDate, h, m)
	kb := a.getMainMenuKeyboard(ctx)
	replyToUpdate(ctx, tgBot, update, tgfmt.Escape(fmt.Sprintf("⏰ Перенесено на %s.", label)+nextFireText), &kb)
	log.Info("reminder postponed to date+time", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderBack(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	uc, err := a.State.GetContext(ctx, userID)
	if err != nil {
		applog.With(ctx, a.Logger).Error("get context", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
		return
	}
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateIdle
		u.ReminderDraft = tgstates.ReminderDraft{}
		u.PendingPostponeReminderID = 0
	})
	text := tgfmt.Join(
		tgfmt.Escape("\n\n📅 Активная дата: "),
		tgfmt.Code(tgfmt.Escape(fmt.Sprintf("%s", uc.ActiveDate))),
		tgfmt.Escape("\n\nВыберите действие:"),
	)
	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, text, &kb)
}

func (a *App) HandleReminderCancel(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderList
	})
	reminders, _ := a.Notifications.ListReminders(ctx, userID)
	kb := tgkeyboards.RemindersList(reminders, 0)
	var text tgfmt.HTML
	if len(reminders) == 0 {
		text = tgfmt.Raw("🔔 Уведомления:\n\nНапоминаний пока нет.")
	} else {
		text = tgfmt.Raw("🔔 Уведомления:")
	}
	replyToCallback(ctx, tgBot, query, text, &kb)
}

// ── Natural language reminder creation ────────────────────────────────────

func (a *App) HandleReminderCreateNL(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateReminderCreateNL
		u.ReminderDraft = tgstates.ReminderDraft{}
		u.ReminderCalMonth = int(now.Month())
		u.ReminderCalYear = now.Year()
	})
	kb := tgkeyboards.ReminderCancel()
	replyToCallback(ctx, tgBot, query,
		tgfmt.Escape("✍️ Опишите напоминание одной фразой.\n\nПримеры:\n• каждый день в 9 утра пить воду\n• каждый понедельник в 9:00 планирование недели\n• 25 числа каждого месяца оплатить аренду\n\nМожно отправить голосовое сообщение."),
		&kb)
}

// handleReminderNLInput processes a natural-language reminder description (from text or voice).
func (a *App) handleReminderNLInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	log := applog.With(ctx, a.Logger)

	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	currentDateTime := now.Format("2006-01-02 15:04")

	processingMsg, _ := tgBot.Send(tgbotapi.NewMessage(chatID, "🧠 Обрабатываю..."))

	result, err := a.LLM.ParseReminder(ctx, text, currentDateTime)
	if err != nil {
		log.Error("LLM parse reminder", zap.Error(err))
		cancelKb := tgkeyboards.ReminderCancel()
		editText(ctx, tgBot, chatID, processingMsg.MessageID,
			tgfmt.Escape("❌ Не удалось обработать запрос. Попробуйте создать напоминание вручную."),
			&cancelKb)
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateReminderList
		})
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
		u.ReminderDraft = tgstates.ReminderDraft{
			Title:        result.Title,
			ScheduleType: result.ScheduleType,
			Hour:         result.Hour,
			Minute:       result.Minute,
			Days:         result.Days,
			DayOfMonth:   result.DayOfMonth,
			Month:        result.Month,
			Day:          result.Day,
			Date:         result.Date,
			IntervalDays: result.IntervalDays,
			CreateTask:   result.CreateTask,
		}
	})

	kb := tgkeyboards.NLReminderConfirm()
	editText(ctx, tgBot, chatID, processingMsg.MessageID, formatNLReminderPreview(result), &kb)
	log.Info("NL reminder parsed", zap.Int64("user_id", userID), zap.String("title", result.Title))
}

// HandleReminderNLConfirm finalizes a reminder that was parsed from natural language.
func (a *App) HandleReminderNLConfirm(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	// Build a fake Update so we can reuse finalizeReminderFromUpdate.
	fakeUpdate := &tgbotapi.Update{Message: query.Message}
	a.finalizeReminderFromUpdate(ctx, tgBot, fakeUpdate, userID)
}
