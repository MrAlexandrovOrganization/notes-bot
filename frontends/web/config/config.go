package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	WebListenAddr    string
	WebPassword      string
	WebSessionSecret string
	// RootID must match the Telegram frontend's ROOT_ID: reminders are
	// scoped by user_id in notifications' storage, and both frontends must
	// use the same user_id to see each other's reminders.
	RootID int64

	TimezoneOffsetHours int
	DayStartHour        int

	CoreGRPCHost          string
	CoreGRPCPort          string
	NotificationsGRPCHost string
	NotificationsGRPCPort string
	SearchGRPCHost        string
	SearchGRPCPort        string
	LLMHost               string
	LLMPort               string
	LLMModel              string
}

func Load() (*Config, error) {
	password := os.Getenv("WEB_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("WEB_PASSWORD is not set")
	}

	secret := os.Getenv("WEB_SESSION_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("WEB_SESSION_SECRET is not set")
	}

	rootIDStr := os.Getenv("ROOT_ID")
	if rootIDStr == "" {
		return nil, fmt.Errorf("ROOT_ID is not set")
	}
	rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("ROOT_ID is not a valid integer: %w", err)
	}

	return &Config{
		WebListenAddr:         envStr("WEB_LISTEN_ADDR", ":8090"),
		WebPassword:           password,
		WebSessionSecret:      secret,
		RootID:                rootID,
		TimezoneOffsetHours:   envInt("TIMEZONE_OFFSET_HOURS", 3),
		DayStartHour:          envInt("DAY_START_HOUR", 7),
		CoreGRPCHost:          envStr("CORE_GRPC_HOST", "localhost"),
		CoreGRPCPort:          envStr("CORE_GRPC_PORT", "50051"),
		NotificationsGRPCHost: envStr("NOTIFICATIONS_GRPC_HOST", "localhost"),
		NotificationsGRPCPort: envStr("NOTIFICATIONS_GRPC_PORT", "50052"),
		SearchGRPCHost:        envStr("SEARCH_GRPC_HOST", "localhost"),
		SearchGRPCPort:        envStr("SEARCH_GRPC_PORT", "50054"),
		LLMHost:               envStr("LLM_HOST", "ollama"),
		LLMPort:               envStr("LLM_PORT", "11434"),
		LLMModel:              envStr("LLM_MODEL", "qwen2.5:7b"),
	}, nil
}

// Validate checks optional configuration values are within valid ranges.
// WEB_PASSWORD and WEB_SESSION_SECRET are already validated by Load().
func (c *Config) Validate() error {
	if c.TimezoneOffsetHours < -12 || c.TimezoneOffsetHours > 14 {
		return fmt.Errorf("TIMEZONE_OFFSET_HOURS must be between -12 and 14, got %d", c.TimezoneOffsetHours)
	}
	if c.DayStartHour < 0 || c.DayStartHour > 23 {
		return fmt.Errorf("DAY_START_HOUR must be between 0 and 23, got %d", c.DayStartHour)
	}
	return nil
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
