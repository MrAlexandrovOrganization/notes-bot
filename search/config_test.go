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
	cfg := &Config{
		DBPassword:           "secret",
		NotesDir:             "/notes",
		IndexInterval:        time.Minute,
		EmbedDim:             1024,
		BackfillBatchPerPass: -1,
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "BACKFILL_BATCH_PER_PASS") {
		t.Fatalf("Validate() error = %v, want BACKFILL_BATCH_PER_PASS error", err)
	}
}
