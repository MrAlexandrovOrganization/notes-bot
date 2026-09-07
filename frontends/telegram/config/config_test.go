package config

import (
	"strings"
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
		{name: "local HTTP needs secret", cfg: Config{LocalAPIURL: "http://telegram-bot-api:8081", WebhookURL: "http://notes-bot-telegram:8080/webhook"}, wantErr: true},
		{name: "maximum secret length", cfg: Config{WebhookURL: "https://example.com/hook", WebhookSecret: strings.Repeat("a", 256)}},
		{name: "secret too long", cfg: Config{WebhookURL: "https://example.com/hook", WebhookSecret: strings.Repeat("a", 257)}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantErr, tt.cfg.Validate() != nil)
		})
	}
}

func TestValidateWebhookURLs(t *testing.T) {
	for _, local := range []string{"", "http://telegram-bot-api:8081", "https://telegram-bot-api:8443/"} {
		for _, webhook := range []string{"", "https://example.com/webhook", "http://notes-bot-telegram:8080/webhook"} {
			t.Run(local+"/"+webhook, func(t *testing.T) {
				cfg := Config{LocalAPIURL: local, WebhookURL: webhook, WebhookSecret: "valid_Secret-123"} // gitleaks:allow
				wantErr := local == "" && strings.HasPrefix(webhook, "http:")
				assert.Equal(t, wantErr, cfg.Validate() != nil)
			})
		}
	}
}

func TestValidateInvalidURLs(t *testing.T) {
	invalid := []string{
		"not-a-url", "/webhook", "//example.com/webhook", "https:///webhook",
		"https://:8080/webhook", "https:example.com", "ftp://example.com",
		"https://exa mple.com", "https://example.com:bad", "https://[::1",
		"https://example.com/%zz", "https://user:password@example.com",
		"https://example.com/#fragment", "https://example.com/#",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			cfg := Config{LocalAPIURL: "http://telegram-bot-api:8081", WebhookURL: raw, WebhookSecret: "valid-secret"}
			assert.Error(t, cfg.Validate())
			cfg.LocalAPIURL, cfg.WebhookURL = raw, "http://notes-bot-telegram:8080/webhook"
			assert.Error(t, cfg.Validate())
		})
	}
	for _, raw := range []string{
		"http://telegram-bot-api:8081/bot123", "http://telegram-bot-api:8081//",
		"http://telegram-bot-api:8081/%2F", "http://telegram-bot-api:8081?key=value",
		"http://telegram-bot-api:8081?",
	} {
		t.Run(raw, func(t *testing.T) {
			// Invalid local origins must also fail in polling and HTTPS webhook modes.
			for _, webhook := range []string{"", "https://example.com/webhook", "http://notes-bot-telegram:8080/webhook"} {
				cfg := Config{LocalAPIURL: raw, WebhookURL: webhook, WebhookSecret: "valid-secret"}
				assert.Error(t, cfg.Validate())
			}
		})
	}
}
