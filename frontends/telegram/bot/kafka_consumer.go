package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// RunKafkaConsumer reads from the reminders_due Kafka topic and sends Telegram notifications.
// It retries on error with a fixed 5-second delay.

type reminderEvent struct {
	UserID     int64  `json:"user_id"`
	Title      string `json:"title"`
	ReminderID int64  `json:"reminder_id"`
	CreateTask bool   `json:"create_task"`
	TodayDate  string `json:"today_date"`
}

func buildReminderKeyboard(reminderID int64, createTask bool, todayDate string) tgbotapi.InlineKeyboardMarkup {
	doneCB := fmt.Sprintf("reminder:done:%d:0", reminderID)
	if createTask && todayDate != "" {
		doneCB = fmt.Sprintf("reminder:done:%d:1:%s", reminderID, todayDate)
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Принято", doneCB),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("+1 ч", fmt.Sprintf("reminder:postpone_hours:1:%d", reminderID)),
			tgbotapi.NewInlineKeyboardButtonData("+3 ч", fmt.Sprintf("reminder:postpone_hours:3:%d", reminderID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("+1 д", fmt.Sprintf("reminder:postpone:1:%d", reminderID)),
			tgbotapi.NewInlineKeyboardButtonData("+3 д", fmt.Sprintf("reminder:postpone:3:%d", reminderID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Выбрать дату", fmt.Sprintf("reminder:custom_date:%d", reminderID)),
		),
	)
}

func RunKafkaConsumer(ctx context.Context, bootstrapServers string, tgBot *tgbotapi.BotAPI, logger *zap.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     []string{bootstrapServers},
			Topic:       "reminders_due",
			GroupID:     "telegram-bot",
			StartOffset: kafka.LastOffset,
		})

		logger.Info("kafka consumer started")
		if err := consume(ctx, r, tgBot, logger); err != nil {
			logger.Warn("kafka consumer error, retrying in 5s", zap.Error(err))
			r.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		r.Close()
		return
	}
}

func consume(ctx context.Context, r *kafka.Reader, tgBot *tgbotapi.BotAPI, logger *zap.Logger) error {
	defer r.Close()
	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			return err
		}

		var ev reminderEvent
		if err := json.Unmarshal(msg.Value, &ev); err != nil {
			logger.Error("failed to parse reminder event", zap.Error(err))
		} else {
			keyboard := buildReminderKeyboard(ev.ReminderID, ev.CreateTask, ev.TodayDate)
			sendMsg := tgbotapi.NewMessage(ev.UserID, fmt.Sprintf("🔔 Напоминание: %s", ev.Title))
			sendMsg.ReplyMarkup = keyboard
			if _, err := tgBot.Send(sendMsg); err != nil {
				logger.Error("failed to send reminder", zap.Int64("user_id", ev.UserID), zap.Error(err))
			} else {
				logger.Info("sent reminder", zap.Int64("user_id", ev.UserID), zap.Int64("reminder_id", ev.ReminderID))
			}
		}

		if err := r.CommitMessages(ctx, msg); err != nil {
			logger.Error("commit kafka message", zap.Error(err))
		}
	}
}
