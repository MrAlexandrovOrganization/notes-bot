package tghandlers

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"

	"notes_bot/frontends/telegram/bot"
	"notes_bot/frontends/telegram/tgkeyboards"
)

// MakeReminderHandler returns a Kafka event handler that sends a Telegram notification
// for each fired reminder. The returned func is safe to pass to bot.RunKafkaConsumer.
func (a *App) MakeReminderHandler(tgBot *tgbotapi.BotAPI) func(context.Context, bot.ReminderEvent) {
	return func(ctx context.Context, ev bot.ReminderEvent) {
		kb := tgkeyboards.ReminderNotification(ev.ReminderID, ev.CreateTask, ev.TodayDate)
		text := fmt.Sprintf("🔔 Напоминание: %s", ev.Title)
		if err := sendText(tgBot, ev.UserID, text, &kb, false); err != nil {
			a.Logger.Error("failed to send reminder notification",
				zap.Int64("user_id", ev.UserID),
				zap.Int64("reminder_id", ev.ReminderID),
				zap.Error(err),
			)
			return
		}
		a.Logger.Info("sent reminder notification",
			zap.Int64("user_id", ev.UserID),
			zap.Int64("reminder_id", ev.ReminderID),
		)
	}
}
