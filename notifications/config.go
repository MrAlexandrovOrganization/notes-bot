package notifications

import (
	"fmt"
	"os"
	"strconv"
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
	SchedulerIntervalSecs int
}

func getEnvStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func LoadConfig() *Config {
	return &Config{
		DBHost:                getEnvStr("DB_HOST", "localhost"),
		DBPort:                getEnvStr("DB_PORT", "5432"),
		DBName:                getEnvStr("DB_NAME", "notifications"),
		DBUser:                getEnvStr("DB_USER", "notif"),
		DBPassword:            getEnvStr("DB_PASSWORD", ""),
		GRPCPort:              getEnvStr("GRPC_PORT", "50052"),
		KafkaBootstrapServers: getEnvStr("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092"),
		CoreGRPCHost:          getEnvStr("CORE_GRPC_HOST", "localhost"),
		CoreGRPCPort:          getEnvStr("CORE_GRPC_PORT", "50051"),
		TimezoneOffsetHours:   getEnvInt("TIMEZONE_OFFSET_HOURS", 3),
		SchedulerIntervalSecs: getEnvInt("SCHEDULER_INTERVAL_SECONDS", 60),
	}
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBPassword,
	)
}
