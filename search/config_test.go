package search

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigUsesBoundedBackfillDefault(t *testing.T) {
	t.Setenv("BACKFILL_BATCH_PER_PASS", "")

	cfg := LoadConfig()
	if cfg.BackfillBatchPerPass != defaultBackfillBatchPerPass {
		t.Fatalf("BackfillBatchPerPass = %d, want %d", cfg.BackfillBatchPerPass, defaultBackfillBatchPerPass)
	}
}

func TestConfigValidateRejectsNegativeBackfillLimit(t *testing.T) {
	cfg := validTestConfig()
	cfg.BackfillBatchPerPass = -1

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "BACKFILL_BATCH_PER_PASS") {
		t.Fatalf("Validate() error = %v, want BACKFILL_BATCH_PER_PASS error", err)
	}
}

func TestConfigValidateAllowsProfilesWithoutEmbeddings(t *testing.T) {
	cfg := validTestConfig()
	cfg.Features.Profiles = true
	cfg.ProfileModel = "small"
	cfg.Products.AskRetrievers = []string{RetrieverLexical, RetrieverProfiles}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, profiles must be independent from embeddings", err)
	}
}

func TestConfigValidateRejectsProfileEmbeddingsWithoutDependencies(t *testing.T) {
	cfg := validTestConfig()
	cfg.Features.ProfileEmbeddings = true
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "FEATURE_PROFILE_EMBEDDINGS requires") {
		t.Fatalf("Validate() error = %v, want profile embedding dependency error", err)
	}
}

func TestConfigValidateRejectsProfileOnlyAskPipeline(t *testing.T) {
	cfg := validTestConfig()
	cfg.Features.Profiles = true
	cfg.ProfileModel = "small"
	cfg.Products.AskRetrievers = []string{RetrieverProfiles}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "source retrieval") {
		t.Fatalf("Validate() error = %v, want source retrieval error", err)
	}
}

func TestLoadConfigBuildsLexicalOnlyDefaultsWhenNeuralFeaturesAreOff(t *testing.T) {
	t.Setenv("FEATURE_EMBEDDINGS", "false")
	t.Setenv("FEATURE_PROFILES", "false")
	t.Setenv("FIND_RETRIEVERS", "")
	t.Setenv("ASK_RETRIEVERS", "")
	cfg := LoadConfig()
	if got := strings.Join(cfg.Products.FindRetrievers, ","); got != "name,lexical" {
		t.Fatalf("FindRetrievers = %q", got)
	}
	if got := strings.Join(cfg.Products.AskRetrievers, ","); got != "lexical" {
		t.Fatalf("AskRetrievers = %q", got)
	}
}

func validTestConfig() *Config {
	return &Config{
		DBPassword: "secret", NotesDir: "/notes", IndexInterval: time.Minute,
		EmbedDim: 1024, BackfillBatchPerPass: 10, ProfileBackfillBatchPerPass: 10,
		AgentMaxSteps: 3, Features: FeatureConfig{LLMGeneration: false},
		Products: ProductConfig{FindRetrievers: []string{RetrieverName, RetrieverLexical}, AskRetrievers: []string{RetrieverLexical}},
	}
}
