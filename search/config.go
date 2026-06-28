package search

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBHost      string
	DBPort      string
	DBName      string
	DBUser      string
	DBPassword  string
	GRPCPort    string
	MetricsPort string

	NotesDir   string
	IgnoreDirs []string

	LLMHost    string
	LLMPort    string
	EmbedModel string

	IndexInterval time.Duration

	// EmbedDim is the expected embedding vector dimension for the configured
	// EmbedModel. The DB schema column type is vector(EmbedDim); changing this
	// after initial schema creation requires a manual migration.
	EmbedDim int

	// EnableEmbeddings toggles the chunking + embedding pipeline.
	// Commit 1 ships with this off — semantic search lands in commit 2.
	EnableEmbeddings bool

	// BackfillBatchPerPass caps how many embedding-less notes the indexer
	// processes per tick. 0 = no cap (drain the queue in one pass).
	BackfillBatchPerPass int
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

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getEnvStrSlice(key string, def []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func LoadConfig() *Config {
	return &Config{
		DBHost:           getEnvStr("DB_HOST", "localhost"),
		DBPort:           getEnvStr("DB_PORT", "5432"),
		DBName:           getEnvStr("DB_NAME", "search"),
		DBUser:           getEnvStr("DB_USER", "search"),
		DBPassword:       getEnvStr("DB_PASSWORD", ""),
		GRPCPort:         getEnvStr("GRPC_PORT", "50054"),
		MetricsPort:      getEnvStr("METRICS_PORT", "9103"),
		NotesDir:         getEnvStr("NOTES_DIR", "/notes"),
		IgnoreDirs:       getEnvStrSlice("INDEX_IGNORE_DIRS", []string{".obsidian", ".trash"}),
		LLMHost:          getEnvStr("LLM_HOST", "ollama"),
		LLMPort:          getEnvStr("LLM_PORT", "11434"),
		EmbedModel:       getEnvStr("EMBED_MODEL", "bge-m3:567m"),
		IndexInterval:    getEnvDuration("INDEX_INTERVAL", 5*time.Minute),
		EmbedDim:             getEnvInt("EMBED_DIM", 1024),
		EnableEmbeddings:     getEnvBool("ENABLE_EMBEDDINGS", false),
		BackfillBatchPerPass: getEnvInt("BACKFILL_BATCH_PER_PASS", 0),
	}
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBPassword,
	)
}

func (c *Config) Validate() error {
	if c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required")
	}
	if c.NotesDir == "" {
		return fmt.Errorf("NOTES_DIR is required")
	}
	if c.IndexInterval <= 0 {
		return fmt.Errorf("INDEX_INTERVAL must be positive, got %s", c.IndexInterval)
	}
	if c.EmbedDim <= 0 {
		return fmt.Errorf("EMBED_DIM must be positive, got %d", c.EmbedDim)
	}
	return nil
}
