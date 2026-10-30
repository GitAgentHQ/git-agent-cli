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

func TestResolve_ZeroConfigDoesNotGuessProvider(t *testing.T) {
	// Session env vars never set the generation model: even when the pi/Claude
	// Code/Codex runtime injects a session model, zero-config resolution keeps
	// Model empty (the session model is captured separately in SessionModel).
	t.Setenv("PI_MODEL", "gemini-3.6-flash-high")
	t.Setenv("MODEL", "gpt-5")
	flags := config.ProviderConfig{}
	got, err := config.Resolve(context.Background(), flags, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != "" {
		t.Errorf("expected empty BaseURL, got %q", got.BaseURL)
	}
	if got.Model != "" {
		t.Errorf("expected empty Model, got %q", got.Model)
	}
}

// TestResolve_SessionEnvModelIgnored locks in the contract that agent-session
// environment variables (PI_MODEL, CLAUDE_CODE_MODEL, CODEX_MODEL, ...)
// NEVER influence the generation model: it resolves only from the --model flag,
// git config --local git-agent.model, or the user config file. A session-injected
// model must not silently override a configured endpoint model — e.g. swapping a
// fast config model for a slow reasoning model routed through a local proxy.
func TestResolve_SessionEnvModelIgnored(t *testing.T) {
	for _, env := range []string{
		"PI_MODEL",
		"CLAUDE_CODE_MODEL",
		"CLAUDE_MODEL",
		"ANTHROPIC_MODEL",
		"CODEX_MODEL",
		"OPENAI_MODEL",
		"MODEL",
	} {
		t.Setenv(env, "opencode/deepseek-v4-flash")
	}
	path := writeTempConfig(t, "api_key: \"file-key\"\nbase_url: \"https://api.example.com/v1\"\nmodel: \"gpt-4\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "gpt-4" {
		t.Errorf("expected config model %q to win over session env, got %q", "gpt-4", got.Model)
	}
	if got.SessionModel != "opencode/deepseek-v4-flash" {
		t.Errorf("expected SessionModel from PI_MODEL env %q, got %q", "opencode/deepseek-v4-flash", got.SessionModel)
	}
}

// TestResolve_SessionModelFeedsAttributionOnly locks in the split: the session
// model is captured for Co-Authored-By attribution but must never displace the
// configured inference model. With no file config, Model stays empty while
// SessionModel carries the PI_MODEL value.
func TestResolve_SessionModelFeedsAttributionOnly(t *testing.T) {
	t.Setenv("PI_MODEL", "gemini-3.6-flash-high")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "" {
		t.Errorf("expected empty inference Model, got %q", got.Model)
	}
	if got.SessionModel != "gemini-3.6-flash-high" {
		t.Errorf("expected SessionModel %q, got %q", "gemini-3.6-flash-high", got.SessionModel)
	}
}

// TestResolve_GenericModelEnvNotCapturedForAttribution locks in that the bare
// MODEL variable (set freely by shells, CI, and unrelated tooling) never feeds
// Co-Authored-By attribution — only agent-scoped session env vars do.
func TestResolve_GenericModelEnvNotCapturedForAttribution(t *testing.T) {
	for _, env := range []string{
		"PI_MODEL",
		"CLAUDE_CODE_MODEL",
		"CLAUDE_MODEL",
		"ANTHROPIC_MODEL",
		"CODEX_MODEL",
		"OPENAI_MODEL",
	} {
		t.Setenv(env, "")
	}
	t.Setenv("MODEL", "gpt-5")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SessionModel != "" {
		t.Errorf("expected empty SessionModel (generic MODEL excluded), got %q", got.SessionModel)
	}
	if got.Model != "" {
		t.Errorf("expected empty Model, got %q", got.Model)
	}
}

// TestResolve_NoBuiltInEndpoint locks in the absence of a built-in endpoint:
// with no flag, no git config, and no config file, base_url stays empty, so the
// caller reports a configuration error instead of routing somewhere.
func TestResolve_NoBuiltInEndpoint(t *testing.T) {
	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != "" {
		t.Errorf("expected no built-in endpoint, got %q", got.BaseURL)
	}
	if got.APIKey != "" {
		t.Errorf("expected no built-in credential, got %q", got.APIKey)
	}
}

// TestResolve_UserBaseURLIsUsed: a configured base_url is the only source of
// the endpoint.
func TestResolve_UserBaseURLIsUsed(t *testing.T) {
	path := writeTempConfig(t, "base_url: \"https://api.example.com/v1\"\n")
	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BaseURL != "https://api.example.com/v1" {
		t.Errorf("expected the configured base_url, got %q", got.BaseURL)
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

func TestResolve_CloudflareAIGatewayIDFromFile(t *testing.T) {
	t.Setenv("TEST_CF_GATEWAY_ID", "git-agent-production")
	path := writeTempConfig(t, "api_key: \"file-key\"\ncloudflare_ai_gateway_id: \"${TEST_CF_GATEWAY_ID}\"\n")

	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CloudflareAIGatewayID != "git-agent-production" {
		t.Errorf("expected CloudflareAIGatewayID %q, got %q", "git-agent-production", got.CloudflareAIGatewayID)
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

// TestResolve_CloudflareAIGatewayIDSurvivesEndpointResolution: the AI Gateway id
// is a routing modifier on the user's own endpoint, so it always stays.
func TestResolve_CloudflareAIGatewayIDSurvivesEndpointResolution(t *testing.T) {
	path := writeTempConfig(t, "base_url: \"https://api.example.com/v1\"\nmodel: \"gpt-4\"\ncloudflare_ai_gateway_id: \"my-gateway\"\n")
	got, err := config.Resolve(context.Background(), config.ProviderConfig{}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CloudflareAIGatewayID != "my-gateway" {
		t.Errorf("expected the configured gateway id, got %q", got.CloudflareAIGatewayID)
	}
	if got.BaseURL != "https://api.example.com/v1" {
		t.Errorf("expected the configured endpoint, got %q", got.BaseURL)
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

func TestKeyRegistry_CloudflareAIGatewayID_UserScopeOnly(t *testing.T) {
	def, ok := config.KeyRegistry["cloudflare_ai_gateway_id"]
	if !ok {
		t.Fatal("expected cloudflare_ai_gateway_id in KeyRegistry")
	}
	if def.Type != "string" || !def.AllowUser || def.AllowProject || def.AllowLocal {
		t.Errorf("expected user-only string key, got type=%q user=%v project=%v local=%v",
			def.Type, def.AllowUser, def.AllowProject, def.AllowLocal)
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
		{"cloudflare-ai-gateway-id", "cloudflare_ai_gateway_id"},
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
