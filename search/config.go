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
	Features      FeatureConfig
	Products      ProductConfig

	// EmbedDim is the expected embedding vector dimension for the configured
	// EmbedModel. The DB schema column type is vector(EmbedDim); changing this
	// after initial schema creation requires a manual migration.
	EmbedDim int

	// BackfillBatchPerPass caps how many embedding-less notes the indexer
	// processes per tick. 0 = no cap (drain the queue in one pass).
	BackfillBatchPerPass int

	// ProfileModel is the Ollama chat model used for structured extraction.
	ProfileModel string

	// ProfileBackfillBatchPerPass caps LLM profile extraction work per sync.
	// 0 drains the backlog; keep this small on CPU-only Ollama installations.
	ProfileBackfillBatchPerPass int

	// AgentMaxSteps limits review/retrieval iterations for one question.
	AgentMaxSteps int
}

// FeatureConfig describes materialized technical capabilities. A true flag is
// a desired-state contract: the indexer continuously backfills that artifact,
// whether or not a product pipeline currently consumes it.
type FeatureConfig struct {
	Embeddings        bool
	VectorIndex       bool
	Profiles          bool
	ProfileEmbeddings bool
	LLMGeneration     bool
}

// ProductConfig selects retrieval capabilities for user-facing operations.
// It never turns materializers on or off; Validate checks that explicitly
// requested retrievers are backed by enabled features.
type ProductConfig struct {
	FindRetrievers []string
	AskRetrievers  []string
}

const (
	RetrieverName     = "name"
	RetrieverLexical  = "lexical"
	RetrieverDense    = "dense"
	RetrieverProfiles = "profiles"
)

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

func getEnvBoolFallback(primary, legacy string, def bool) bool {
	if os.Getenv(primary) != "" {
		return getEnvBool(primary, def)
	}
	return getEnvBool(legacy, def)
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
	features := FeatureConfig{
		Embeddings:    getEnvBoolFallback("FEATURE_EMBEDDINGS", "ENABLE_EMBEDDINGS", false),
		Profiles:      getEnvBoolFallback("FEATURE_PROFILES", "ENABLE_PROFILES", false),
		LLMGeneration: getEnvBool("FEATURE_LLM_GENERATION", true),
	}
	features.VectorIndex = getEnvBool("FEATURE_VECTOR_INDEX", features.Embeddings)
	features.ProfileEmbeddings = getEnvBool("FEATURE_PROFILE_EMBEDDINGS", features.Embeddings && features.Profiles)

	defaultAskRetrievers := []string{RetrieverLexical}
	if features.Embeddings {
		defaultAskRetrievers = append(defaultAskRetrievers, RetrieverDense)
	}
	if features.Profiles {
		defaultAskRetrievers = append(defaultAskRetrievers, RetrieverProfiles)
	}

	return &Config{
		DBHost:        getEnvStr("DB_HOST", "localhost"),
		DBPort:        getEnvStr("DB_PORT", "5432"),
		DBName:        getEnvStr("DB_NAME", "search"),
		DBUser:        getEnvStr("DB_USER", "search"),
		DBPassword:    getEnvStr("DB_PASSWORD", ""),
		GRPCPort:      getEnvStr("GRPC_PORT", "50054"),
		MetricsPort:   getEnvStr("METRICS_PORT", "9103"),
		NotesDir:      getEnvStr("NOTES_DIR", "/notes"),
		IgnoreDirs:    getEnvStrSlice("INDEX_IGNORE_DIRS", []string{".obsidian", ".trash"}),
		LLMHost:       getEnvStr("LLM_HOST", "ollama"),
		LLMPort:       getEnvStr("LLM_PORT", "11434"),
		EmbedModel:    getEnvStr("EMBED_MODEL", "bge-m3:567m"),
		AgentModel:    getEnvStr("AGENT_MODEL", "qwen2.5:7b"),
		IndexInterval: getEnvDuration("INDEX_INTERVAL", 5*time.Minute),
		EmbedDim:      getEnvInt("EMBED_DIM", 1024),
		Features:      features,
		Products: ProductConfig{
			FindRetrievers: getEnvStrSlice("FIND_RETRIEVERS", []string{RetrieverName, RetrieverLexical}),
			AskRetrievers:  getEnvStrSlice("ASK_RETRIEVERS", defaultAskRetrievers),
		},
		BackfillBatchPerPass: getEnvInt("BACKFILL_BATCH_PER_PASS", defaultBackfillBatchPerPass),
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
	if c.Features.VectorIndex && !c.Features.Embeddings {
		return fmt.Errorf("FEATURE_VECTOR_INDEX requires FEATURE_EMBEDDINGS")
	}
	if c.Features.ProfileEmbeddings && (!c.Features.Profiles || !c.Features.Embeddings) {
		return fmt.Errorf("FEATURE_PROFILE_EMBEDDINGS requires FEATURE_PROFILES and FEATURE_EMBEDDINGS")
	}
	if c.Features.Profiles && c.ProfileModel == "" {
		return fmt.Errorf("PROFILE_MODEL is required when profiles are enabled")
	}
	if c.Features.LLMGeneration && c.AgentModel == "" {
		return fmt.Errorf("AGENT_MODEL is required when LLM generation is enabled")
	}
	if c.AgentMaxSteps < 1 || c.AgentMaxSteps > 5 {
		return fmt.Errorf("AGENT_MAX_STEPS must be between 1 and 5, got %d", c.AgentMaxSteps)
	}
	if err := validateRetrievers("FIND_RETRIEVERS", c.Products.FindRetrievers,
		map[string]bool{RetrieverName: true, RetrieverLexical: true, RetrieverDense: c.Features.Embeddings}); err != nil {
		return err
	}
	if err := validateRetrievers("ASK_RETRIEVERS", c.Products.AskRetrievers,
		map[string]bool{RetrieverLexical: true, RetrieverDense: c.Features.Embeddings, RetrieverProfiles: c.Features.Profiles}); err != nil {
		return err
	}
	if !c.UsesAskRetriever(RetrieverLexical) && !c.UsesAskRetriever(RetrieverDense) {
		return fmt.Errorf("ASK_RETRIEVERS must contain lexical or dense source retrieval")
	}
	return nil
}

func validateRetrievers(field string, values []string, available map[string]bool) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must contain at least one retriever", field)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, known := available[value]; !known {
			return fmt.Errorf("%s contains unsupported retriever %q", field, value)
		}
		if !available[value] {
			return fmt.Errorf("%s requests disabled retriever %q", field, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s contains duplicate retriever %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (c *Config) UsesFindRetriever(name string) bool {
	return slicesContains(c.Products.FindRetrievers, name)
}

func (c *Config) UsesAskRetriever(name string) bool {
	return slicesContains(c.Products.AskRetrievers, name)
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
