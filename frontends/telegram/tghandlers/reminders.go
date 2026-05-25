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
		lines = append(lines, tgfmt.Escape(fmt.Sprintf("• %s (%s) — %s",
			r.Title,
			scheduleLabel(r.ScheduleType),
			timeutil.FormatLocalTime(r.NextFireAt, tzOffset),
		)))
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
		tgfmt.Escape(fmt.Sprintf("Название: %s\n\nВыберите тип расписания:", text)),
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
			tgfmt.Pre(tgfmt.Escape(title)), taskNote, tgfmt.Raw("\n"),
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
		uc, err := a.State.GetContext(ctx, userID)
		if err != nil {
			applog.With(ctx, a.Logger).Error("get context", zap.Error(err))
			replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Произошла ошибка."), nil)
			return
		}
		reminderID := uc.PendingPostponeReminderID
		if reminderID != 0 {
			if _, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, 0, dateStr, 0); err != nil {
				applog.With(ctx, a.Logger).Error("postpone reminder", zap.Error(err))
				replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
				return
			}
		}
		a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
			u.State = tgstates.StateIdle
			u.PendingPostponeReminderID = 0
		})
		replyToCallback(ctx, tgBot, query, tgfmt.Escape(fmt.Sprintf("✅ Напоминание перенесено на %s.", dateStr)), nil)
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

func (a *App) HandleReminderPostponeDays(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, days, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, int32(days), "", 0)
	if err != nil {
		applog.With(ctx, a.Logger).Error("postpone reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}
	a.sendPostponeResult(ctx, tgBot, query, result, days, "д", userID, reminderID)
}

func (a *App) HandleReminderPostponeHours(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, hours, reminderID int64) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	result, err := a.Notifications.PostponeReminder(ctx, reminderID, userID, 0, "", int32(hours))
	if err != nil {
		applog.With(ctx, a.Logger).Error("postpone reminder", zap.Error(err))
		replyToCallback(ctx, tgBot, query, tgfmt.Escape("❌ Ошибка при переносе напоминания."), nil)
		return
	}
	a.sendPostponeResult(ctx, tgBot, query, result, hours, "ч", userID, reminderID)
}

func (a *App) sendPostponeResult(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, result *clients.ReminderInfo, amount int64, unit string, userID, reminderID int64) {
	log := applog.With(ctx, a.Logger)
	log.Debug("sendPostponeResult")
	nextFireText := ""
	if result != nil {
		nextFire := timeutil.FormatLocalTime(result.NextFireAt, a.Cfg.TimezoneOffsetHours)
		if nextFire != "" {
			nextFireText = fmt.Sprintf(" (следующее: %s)", nextFire)
		}
	}
	original := ""
	if query.Message != nil {
		original = query.Message.Text
	}
	text := tgfmt.Escape(fmt.Sprintf("%s\n\n⏰ Перенесено на %d %s.", original, amount, unit) + nextFireText)

	kb := a.getMainMenuKeyboard(ctx)
	replyToCallback(ctx, tgBot, query, text, &kb)
	log.Info("reminder postponed", zap.Int64("user_id", userID), zap.Int64("reminder_id", reminderID))
}

func (a *App) HandleReminderCustomDate(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64, reminderID int64) {
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
