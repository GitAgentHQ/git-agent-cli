package cmd_test

import (
	"testing"

	"github.com/gitagenthq/git-agent/cmd"
	infraConfig "github.com/gitagenthq/git-agent/infrastructure/config"
)

// TestProviderConfigError_FreeGateway allows zero-config free mode: an empty
// api_key + a base_url (the built-in shared gateway) passes without a model,
// because the Worker pins the model server-side.
func TestProviderConfigError_FreeGateway(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{BaseURL: "https://git-agent-gateway.example.com"}
	if got := cmd.ProviderConfigErrorForTest(cfg); got != "" {
		t.Errorf("expected free-gateway config to pass, got error: %q", got)
	}
}

// TestProviderConfigError_NoProvider reports an actionable error when nothing
// is configured (dev build with no built-in gateway URL).
func TestProviderConfigError_NoProvider(t *testing.T) {
	cfg := &infraConfig.ProviderConfig{}
	if got := cmd.ProviderConfigErrorForTest(cfg); got == "" {
		t.Fatal("expected error when no provider is configured, got none")
	}
}

// TestProviderConfigError_NilConfig exercises the nil branch: a nil config
// must not panic and must return an error.
func TestProviderConfigError_NilConfig(t *testing.T) {
	if got := cmd.ProviderConfigErrorForTest(nil); got == "" {
		t.Fatal("expected error for nil config, got none")
	}
}

// TestProviderConfigError_DirectRequiresBaseURLAndModel: when a user supplies
// an api_key (direct mode), base_url and model must both be present.
func TestProviderConfigError_DirectRequiresBaseURLAndModel(t *testing.T) {
	// api_key set, but no base_url/model → incomplete.
	incomplete := &infraConfig.ProviderConfig{APIKey: "sk-test"}
	if got := cmd.ProviderConfigErrorForTest(incomplete); got == "" {
		t.Fatal("expected incomplete-config error when api_key set but base_url/model missing")
	}
	// api_key + base_url + model → valid direct mode.
	complete := &infraConfig.ProviderConfig{
		APIKey:  "sk-test",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o",
	}
	if got := cmd.ProviderConfigErrorForTest(complete); got != "" {
		t.Errorf("expected complete direct config to pass, got error: %q", got)
	}
}
