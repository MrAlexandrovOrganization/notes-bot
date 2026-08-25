package config

import (
	"fmt"
	"os"
	"strconv"

	"notes-bot/internal/env"
)

type Config struct {
	BOTToken              string
	RootID                int64
	TimezoneOffsetHours   int
	DayStartHour          int
	CoreGRPCHost          string
	CoreGRPCPort          string
	NotificationsGRPCHost string
	NotificationsGRPCPort string
	WhisperGRPCHost       string
	WhisperGRPCPort       string
	SearchGRPCHost        string
	SearchGRPCPort        string
	KafkaBootstrapServers string
	RedisHost             string
	RedisPort             string
	LLMHost               string
	LLMPort               string
	LLMModel              string
	LocationHost          string
	LocationPort          string
	LocalAPIURL           string

	// Webhook mode: if WebhookURL is set, bot runs in webhook mode instead of polling.
	// WebhookURL must be an HTTPS URL reachable by Telegram (e.g. https://bot.example.com/webhook).
	WebhookURL        string
	WebhookListenAddr string
	WebhookSecret     string
}

func Load() (*Config, error) {
	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		return nil, fmt.Errorf("BOT_TOKEN is not set")
	}

	rootIDStr := os.Getenv("ROOT_ID")
	if rootIDStr == "" {
		return nil, fmt.Errorf("ROOT_ID is not set")
	}
	rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("ROOT_ID is not a valid integer: %w", err)
	}
	if rootID <= 0 {
		return nil, fmt.Errorf("ROOT_ID must be a positive Telegram user id, got %d", rootID)
	}

	return &Config{
		BOTToken:              botToken,
		RootID:                rootID,
		TimezoneOffsetHours:   env.Int("TIMEZONE_OFFSET_HOURS", 3),
		DayStartHour:          env.Int("DAY_START_HOUR", 7),
		CoreGRPCHost:          env.Str("CORE_GRPC_HOST", "localhost"),
		CoreGRPCPort:          env.Str("CORE_GRPC_PORT", "50051"),
		NotificationsGRPCHost: env.Str("NOTIFICATIONS_GRPC_HOST", "localhost"),
		NotificationsGRPCPort: env.Str("NOTIFICATIONS_GRPC_PORT", "50052"),
		WhisperGRPCHost:       env.Str("WHISPER_GRPC_HOST", "localhost"),
		WhisperGRPCPort:       env.Str("WHISPER_GRPC_PORT", "50053"),
		SearchGRPCHost:        env.Str("SEARCH_GRPC_HOST", "localhost"),
		SearchGRPCPort:        env.Str("SEARCH_GRPC_PORT", "50054"),
		KafkaBootstrapServers: env.Str("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092"),
		RedisHost:             env.Str("REDIS_HOST", "localhost"),
		RedisPort:             env.Str("REDIS_PORT", "6379"),
		LLMHost:               env.Str("LLM_HOST", "ollama"),
		LLMPort:               env.Str("LLM_PORT", "11434"),
		LLMModel:              env.Str("LLM_MODEL", "qwen2.5:7b"),
		LocationHost:          env.Str("LOCATION_HOST", "localhost"),
		LocationPort:          env.Str("LOCATION_PORT", "8080"),
		LocalAPIURL:           env.Str("TELEGRAM_LOCAL_API_URL", ""),
		WebhookURL:            env.Str("WEBHOOK_URL", ""),
		WebhookListenAddr:     env.Str("WEBHOOK_LISTEN_ADDR", ":8080"),
		WebhookSecret:         env.Str("TELEGRAM_WEBHOOK_SECRET", ""),
	}, nil
}

// Validate checks optional configuration values are within valid ranges.
// BOT_TOKEN and ROOT_ID are already validated by Load().
func (c *Config) Validate() error {
	if c.TimezoneOffsetHours < -12 || c.TimezoneOffsetHours > 14 {
		return fmt.Errorf("TIMEZONE_OFFSET_HOURS must be between -12 and 14, got %d", c.TimezoneOffsetHours)
	}
	if c.DayStartHour < 0 || c.DayStartHour > 23 {
		return fmt.Errorf("DAY_START_HOUR must be between 0 and 23, got %d", c.DayStartHour)
	}
	if c.WebhookURL != "" && c.WebhookSecret == "" {
		return fmt.Errorf("TELEGRAM_WEBHOOK_SECRET must be set when WEBHOOK_URL is configured")
	}
	if len(c.WebhookSecret) > 256 {
		return fmt.Errorf("TELEGRAM_WEBHOOK_SECRET must not exceed 256 characters")
	}
	for _, r := range c.WebhookSecret {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return fmt.Errorf("TELEGRAM_WEBHOOK_SECRET may contain only A-Z, a-z, 0-9, _ and -")
		}
	}
	return nil
}
