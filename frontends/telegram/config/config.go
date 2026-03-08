package config

import (
	"fmt"
	"os"
	"strconv"
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
	KafkaBootstrapServers string
	RedisHost             string
	RedisPort             string
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

	return &Config{
		BOTToken:              botToken,
		RootID:                rootID,
		TimezoneOffsetHours:   envInt("TIMEZONE_OFFSET_HOURS", 3),
		DayStartHour:          envInt("DAY_START_HOUR", 7),
		CoreGRPCHost:          envStr("CORE_GRPC_HOST", "localhost"),
		CoreGRPCPort:          envStr("CORE_GRPC_PORT", "50051"),
		NotificationsGRPCHost: envStr("NOTIFICATIONS_GRPC_HOST", "localhost"),
		NotificationsGRPCPort: envStr("NOTIFICATIONS_GRPC_PORT", "50052"),
		WhisperGRPCHost:       envStr("WHISPER_GRPC_HOST", "localhost"),
		WhisperGRPCPort:       envStr("WHISPER_GRPC_PORT", "50053"),
		KafkaBootstrapServers: envStr("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092"),
		RedisHost:             envStr("REDIS_HOST", "localhost"),
		RedisPort:             envStr("REDIS_PORT", "6379"),
	}, nil
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
