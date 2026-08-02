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
	AgentModel string

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

	// EnableProfiles builds a compact, structured routing card for every note.
	// Cards are never treated as source evidence: the agent uses them to select
	// notes and then drills down to the original chunks.
	EnableProfiles bool

	// ProfileModel is the Ollama chat model used for structured extraction.
	ProfileModel string

	// ProfileBackfillBatchPerPass caps LLM profile extraction work per sync.
	// 0 drains the backlog; keep this small on CPU-only Ollama installations.
	ProfileBackfillBatchPerPass int

	// AgentMaxSteps limits review/retrieval iterations for one question.
	AgentMaxSteps int
}

const defaultBackfillBatchPerPass = 50
const defaultProfileBackfillBatchPerPass = 10
const defaultAgentMaxSteps = 3

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
		DBHost:               getEnvStr("DB_HOST", "localhost"),
		DBPort:               getEnvStr("DB_PORT", "5432"),
		DBName:               getEnvStr("DB_NAME", "search"),
		DBUser:               getEnvStr("DB_USER", "search"),
		DBPassword:           getEnvStr("DB_PASSWORD", ""),
		GRPCPort:             getEnvStr("GRPC_PORT", "50054"),
		MetricsPort:          getEnvStr("METRICS_PORT", "9103"),
		NotesDir:             getEnvStr("NOTES_DIR", "/notes"),
		IgnoreDirs:           getEnvStrSlice("INDEX_IGNORE_DIRS", []string{".obsidian", ".trash"}),
		LLMHost:              getEnvStr("LLM_HOST", "ollama"),
		LLMPort:              getEnvStr("LLM_PORT", "11434"),
		EmbedModel:           getEnvStr("EMBED_MODEL", "bge-m3:567m"),
		AgentModel:           getEnvStr("AGENT_MODEL", "qwen2.5:7b"),
		IndexInterval:        getEnvDuration("INDEX_INTERVAL", 5*time.Minute),
		EmbedDim:             getEnvInt("EMBED_DIM", 1024),
		EnableEmbeddings:     getEnvBool("ENABLE_EMBEDDINGS", false),
		BackfillBatchPerPass: getEnvInt("BACKFILL_BATCH_PER_PASS", defaultBackfillBatchPerPass),
		EnableProfiles:       getEnvBool("ENABLE_PROFILES", false),
		ProfileModel:         getEnvStr("PROFILE_MODEL", "qwen3.5:2b"),
		ProfileBackfillBatchPerPass: getEnvInt(
			"PROFILE_BACKFILL_BATCH_PER_PASS", defaultProfileBackfillBatchPerPass,
		),
		AgentMaxSteps: getEnvInt("AGENT_MAX_STEPS", defaultAgentMaxSteps),
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
	if c.BackfillBatchPerPass < 0 {
		return fmt.Errorf("BACKFILL_BATCH_PER_PASS must be non-negative, got %d", c.BackfillBatchPerPass)
	}
	if c.ProfileBackfillBatchPerPass < 0 {
		return fmt.Errorf("PROFILE_BACKFILL_BATCH_PER_PASS must be non-negative, got %d", c.ProfileBackfillBatchPerPass)
	}
	if c.EnableProfiles && !c.EnableEmbeddings {
		return fmt.Errorf("ENABLE_PROFILES requires ENABLE_EMBEDDINGS")
	}
	if c.EnableProfiles && c.ProfileModel == "" {
		return fmt.Errorf("PROFILE_MODEL is required when profiles are enabled")
	}
	if c.AgentMaxSteps < 1 || c.AgentMaxSteps > 5 {
		return fmt.Errorf("AGENT_MAX_STEPS must be between 1 and 5, got %d", c.AgentMaxSteps)
	}
	return nil
}
