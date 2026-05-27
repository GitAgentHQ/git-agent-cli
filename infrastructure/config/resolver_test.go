package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitagenthq/git-agent/infrastructure/config"
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

func TestResolve_FreeModeUsesBuildTimeCredentials(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"file-key\"\nbase_url: \"https://api.example.com/v1\"\nmodel: \"gpt-4\"\n")

	// Simulate build-time proxy credentials injected via ldflags.
	orig := config.BuildAPIKey
	config.BuildAPIKey = "proxy-key"
	defer func() { config.BuildAPIKey = orig }()

	flags := config.ProviderConfig{FreeMode: true}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.APIKey != "proxy-key" {
		t.Errorf("expected APIKey %q, got %q", "proxy-key", got.APIKey)
	}
	// YAML file values must be ignored.
	if got.BaseURL == "https://api.example.com/v1" {
		t.Errorf("free mode must not use YAML base_url")
	}
	if got.Model == "gpt-4" {
		t.Errorf("free mode must not use YAML model")
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

func TestKeyRegistry_RequestTimeout_UserScopeOnly(t *testing.T) {
	def, ok := config.KeyRegistry["request_timeout"]
	if !ok {
		t.Fatal("expected request_timeout in KeyRegistry")
	}
	if def.Type != "duration" {
		t.Errorf("expected duration type, got %q", def.Type)
	}
	if !def.AllowUser {
		t.Errorf("expected AllowUser=true")
	}
	if def.AllowProject || def.AllowLocal {
		t.Errorf("expected user-scope only, got project=%v local=%v", def.AllowProject, def.AllowLocal)
	}
}

func TestKeyRegistry_HeartbeatInterval_UserScopeOnly(t *testing.T) {
	def, ok := config.KeyRegistry["heartbeat_interval"]
	if !ok {
		t.Fatal("expected heartbeat_interval in KeyRegistry")
	}
	if def.Type != "duration" {
		t.Errorf("expected duration type, got %q", def.Type)
	}
	if !def.AllowUser {
		t.Errorf("expected AllowUser=true")
	}
	if def.AllowProject || def.AllowLocal {
		t.Errorf("expected user-scope only, got project=%v local=%v", def.AllowProject, def.AllowLocal)
	}
}

func TestKeyRegistry_PlanFallback_ProjectAndLocal(t *testing.T) {
	def, ok := config.KeyRegistry["plan_fallback"]
	if !ok {
		t.Fatal("expected plan_fallback in KeyRegistry")
	}
	if def.Type != "string" {
		t.Errorf("expected string type, got %q", def.Type)
	}
	if !def.AllowProject || !def.AllowLocal {
		t.Errorf("expected project+local scopes, got project=%v local=%v", def.AllowProject, def.AllowLocal)
	}
}

func TestResolveKey_NewKebabAliases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"request-timeout", "request_timeout"},
		{"heartbeat-interval", "heartbeat_interval"},
		{"plan-fallback", "plan_fallback"},
	}
	for _, tc := range cases {
		got, err := config.ResolveKey(tc.in)
		if err != nil {
			t.Errorf("ResolveKey(%q) failed: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolveKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolve_RequestTimeoutAndHeartbeatFromFile(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"k\"\nrequest_timeout: \"45s\"\nheartbeat_interval: \"7s\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequestTimeout != 45*time.Second {
		t.Errorf("expected RequestTimeout 45s, got %v", got.RequestTimeout)
	}
	if got.HeartbeatInterval != 7*time.Second {
		t.Errorf("expected HeartbeatInterval 7s, got %v", got.HeartbeatInterval)
	}
}

func TestResolve_RequestTimeoutAndHeartbeatDefaults(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"k\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequestTimeout != config.DefaultRequestTimeout {
		t.Errorf("expected RequestTimeout default %v, got %v", config.DefaultRequestTimeout, got.RequestTimeout)
	}
	if got.HeartbeatInterval != config.DefaultHeartbeatInterval {
		t.Errorf("expected HeartbeatInterval default %v, got %v", config.DefaultHeartbeatInterval, got.HeartbeatInterval)
	}
}

func TestResolve_FlagOverridesFileForTimeoutAndHeartbeat(t *testing.T) {
	path := writeTempConfig(t, "api_key: \"k\"\nrequest_timeout: \"45s\"\nheartbeat_interval: \"7s\"\n")

	flags := config.ProviderConfig{
		RequestTimeout:    120 * time.Second,
		HeartbeatInterval: 30 * time.Second,
	}
	got, err := config.Resolve(context.Background(), flags, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequestTimeout != 120*time.Second {
		t.Errorf("expected RequestTimeout 120s (flag), got %v", got.RequestTimeout)
	}
	if got.HeartbeatInterval != 30*time.Second {
		t.Errorf("expected HeartbeatInterval 30s (flag), got %v", got.HeartbeatInterval)
	}
}

func TestResolve_DefaultsWhenZeroConfig(t *testing.T) {
	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RequestTimeout != config.DefaultRequestTimeout {
		t.Errorf("expected RequestTimeout default %v, got %v", config.DefaultRequestTimeout, got.RequestTimeout)
	}
	if got.HeartbeatInterval != config.DefaultHeartbeatInterval {
		t.Errorf("expected HeartbeatInterval default %v, got %v", config.DefaultHeartbeatInterval, got.HeartbeatInterval)
	}
}
