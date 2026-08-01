package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateWebhookSecret(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "polling needs no secret", cfg: Config{}},
		{name: "webhook needs secret", cfg: Config{WebhookURL: "https://example.com/hook"}, wantErr: true},
		{name: "valid secret", cfg: Config{WebhookURL: "https://example.com/hook", WebhookSecret: "valid_Secret-123"}},
		{name: "invalid secret character", cfg: Config{WebhookURL: "https://example.com/hook", WebhookSecret: "not valid"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantErr, tt.cfg.Validate() != nil)
		})
	}
}
