package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fradser/git-agent/infrastructure/config"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestResolve_FlagAPIKeyTakesPrecedenceOverFile(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"file-key\"\nbase_url: \"https://api.example.com/v1\"\nmodel: \"gpt-4\"\n")

	flags := config.ProviderConfig{APIKey: "flag-key"}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.APIKey != "flag-key" {
		t.Errorf("expected APIKey %q, got %q", "flag-key", got.APIKey)
	}
}

func TestResolve_FileAPIKeyUsedWhenNoFlag(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"file-key\"\nbase_url: \"https://api.example.com/v1\"\nmodel: \"gpt-4\"\n")

	flags := config.ProviderConfig{}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.APIKey != "file-key" {
		t.Errorf("expected APIKey %q, got %q", "file-key", got.APIKey)
	}
}

func TestResolve_ZeroConfigUsesDefaults(t *testing.T) {
	flags := config.ProviderConfig{}
	got, err := config.Resolve(context.Background(), flags, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != config.DefaultBaseURL {
		t.Errorf("expected BaseURL %q, got %q", config.DefaultBaseURL, got.BaseURL)
	}
	if got.Model != config.DefaultModel {
		t.Errorf("expected Model %q, got %q", config.DefaultModel, got.Model)
	}
}

func TestResolve_FlagModelOverridesFile(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"file-key\"\nmodel: \"gpt-4\"\n")

	flags := config.ProviderConfig{Model: "claude-3-5-haiku-20241022"}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "claude-3-5-haiku-20241022" {
		t.Errorf("expected Model %q, got %q", "claude-3-5-haiku-20241022", got.Model)
	}
}

func TestResolve_EnvVarExpandedInAPIKey(t *testing.T) {
	t.Setenv("TEST_GIT_AGENT_API_KEY", "secret-from-env")
	path := writeTempConfig(t, "api_key: \"${TEST_GIT_AGENT_API_KEY}\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.APIKey != "secret-from-env" {
		t.Errorf("expected APIKey %q, got %q", "secret-from-env", got.APIKey)
	}
}

func TestResolve_EnvVarExpandedInBaseURL(t *testing.T) {
	t.Setenv("TEST_GIT_AGENT_BASE_URL", "https://env.example.com/v1")
	path := writeTempConfig(t, "api_key: \"key\"\nbase_url: \"${TEST_GIT_AGENT_BASE_URL}\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != "https://env.example.com/v1" {
		t.Errorf("expected BaseURL %q, got %q", "https://env.example.com/v1", got.BaseURL)
	}
}

func TestResolve_UnsetEnvVarExpandsToEmpty(t *testing.T) {
	os.Unsetenv("TEST_GIT_AGENT_UNSET_VAR")
	path := writeTempConfig(t, "api_key: \"${TEST_GIT_AGENT_UNSET_VAR}\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.APIKey != "" {
		t.Errorf("expected empty APIKey, got %q", got.APIKey)
	}
}

func TestResolve_FlagBaseURLOverridesFile(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"file-key\"\nbase_url: \"https://api.example.com/v1\"\n")

	flags := config.ProviderConfig{BaseURL: "https://custom.api.com/v1"}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != "https://custom.api.com/v1" {
		t.Errorf("expected BaseURL %q, got %q", "https://custom.api.com/v1", got.BaseURL)
	}
}
