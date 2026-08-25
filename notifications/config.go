package notifications

import (
	"fmt"

	"notes-bot/internal/env"
)

type Config struct {
	DBHost                string
	DBPort                string
	DBName                string
	DBUser                string
	DBPassword            string
	GRPCPort              string
	KafkaBootstrapServers string
	CoreGRPCHost          string
	CoreGRPCPort          string
	TimezoneOffsetHours   int
	DayStartHour          int
	SchedulerIntervalSecs int
}

func LoadConfig() *Config {
	return &Config{
		DBHost:                env.Str("DB_HOST", "localhost"),
		DBPort:                env.Str("DB_PORT", "5432"),
		DBName:                env.Str("DB_NAME", "notifications"),
		DBUser:                env.Str("DB_USER", "notif"),
		DBPassword:            env.Str("DB_PASSWORD", ""),
		GRPCPort:              env.Str("GRPC_PORT", "50052"),
		KafkaBootstrapServers: env.Str("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092"),
		CoreGRPCHost:          env.Str("CORE_GRPC_HOST", "localhost"),
		CoreGRPCPort:          env.Str("CORE_GRPC_PORT", "50051"),
		TimezoneOffsetHours:   env.Int("TIMEZONE_OFFSET_HOURS", 3),
		DayStartHour:          env.Int("DAY_START_HOUR", 7),
		SchedulerIntervalSecs: env.Int("SCHEDULER_INTERVAL_SECONDS", 60),
	}
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBPassword,
	)
}

// Validate checks that required configuration values are present and valid.
// Call this at startup after LoadConfig().
func (c *Config) Validate() error {
	if c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required")
	}
	if c.SchedulerIntervalSecs <= 0 {
		return fmt.Errorf("SCHEDULER_INTERVAL_SECONDS must be positive, got %d", c.SchedulerIntervalSecs)
	}
	if c.TimezoneOffsetHours < -12 || c.TimezoneOffsetHours > 14 {
		return fmt.Errorf("TIMEZONE_OFFSET_HOURS must be between -12 and 14, got %d", c.TimezoneOffsetHours)
	}
	if c.DayStartHour < 0 || c.DayStartHour > 23 {
		return fmt.Errorf("DAY_START_HOUR must be between 0 and 23, got %d", c.DayStartHour)
	}
	return nil
}
