package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultRequestTimeout bounds the per-HTTP-request deadline given to the LLM
// client, including streamed completions. Chosen to comfortably exceed a slow
// 10 KB/s response while still cutting the wire if the upstream stalls.
const DefaultRequestTimeout = 90 * time.Second

// DefaultHeartbeatInterval is the cadence at which the CLI emits "still
// waiting" progress lines while an LLM call is in flight.
const DefaultHeartbeatInterval = 15 * time.Second

type ProviderConfig struct {
	APIKey                  string
	BaseURL                 string
	Model                   string
	JevAPIKey               string // TypeSafe key for the optional System One layer; empty disables that layer
	JevBaseURL              string // empty = typesafe.DefaultBaseURL
	JevModel                string // empty = typesafe.DefaultModel
	JevMode                 string // off | shadow | on; empty = decision.DefaultMode
	JevMinConfidence        float64
	JevMaxCalls             int
	SessionModel            string // active agent session model (PI_MODEL, etc.), used only for Co-Authored-By attribution — never for inference routing
	CloudflareAIGatewayID   string
	RequestTimeout          time.Duration // 0 = use DefaultRequestTimeout
	HeartbeatInterval       time.Duration // 0 = use DefaultHeartbeatInterval
	RequireGitAgentCoAuthor bool          // When true, append Co-Authored-By: Git Agent trailer (opt-in)
	RequireModelCoAuthor    bool          // When true, every commit must carry a Co-Authored-By from an AI-provider domain
	AutoModelCoAuthor       *bool         // When false, disable automatic model co-author inference (nil = true, backward compatible)
}

type fileConfig struct {
	APIKey                  string  `yaml:"api_key"`
	BaseURL                 string  `yaml:"base_url"`
	Model                   string  `yaml:"model"`
	JevAPIKey               string  `yaml:"jev_api_key"`
	JevBaseURL              string  `yaml:"jev_base_url"`
	JevModel                string  `yaml:"jev_model"`
	JevMode                 string  `yaml:"jev_mode"`
	JevMinConfidence        float64 `yaml:"jev_min_confidence"`
	JevMaxCalls             int     `yaml:"jev_max_calls"`
	CloudflareAIGatewayID   string  `yaml:"cloudflare_ai_gateway_id"`
	RequestTimeout          string  `yaml:"request_timeout"`
	HeartbeatInterval       string  `yaml:"heartbeat_interval"`
	RequireGitAgentCoAuthor bool    `yaml:"require_git_agent_co_author"`
	RequireModelCoAuthor    bool    `yaml:"require_model_co_author"`
	AutoModelCoAuthor       *bool   `yaml:"auto_model_co_author"`
}

// Resolve merges config from (highest to lowest priority):
// CLI flags > git config --local git-agent.* > YAML file > empty.
//
// There is no built-in endpoint: an unset base_url or model stays empty, and
// providerConfigError turns that into an actionable message.
func Resolve(ctx context.Context, flags ProviderConfig, configPath string) (*ProviderConfig, error) {
	var file fileConfig
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &file); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to parse config %s: %v\n", configPath, err)
			}
			file.APIKey = os.ExpandEnv(file.APIKey)
			file.BaseURL = os.ExpandEnv(file.BaseURL)
			file.Model = os.ExpandEnv(file.Model)
			file.JevAPIKey = os.ExpandEnv(file.JevAPIKey)
			file.JevBaseURL = os.ExpandEnv(file.JevBaseURL)
			file.JevModel = os.ExpandEnv(file.JevModel)
			file.JevMode = os.ExpandEnv(file.JevMode)
			file.CloudflareAIGatewayID = os.ExpandEnv(file.CloudflareAIGatewayID)
		}
	}

	gitModel, _ := ReadGitConfig(ctx, "model")
	gitBaseURL, _ := ReadGitConfig(ctx, "base-url")

	result := &ProviderConfig{}

	if flags.APIKey != "" {
		result.APIKey = flags.APIKey
	} else if file.APIKey != "" {
		result.APIKey = file.APIKey
	}

	if flags.BaseURL != "" {
		result.BaseURL = flags.BaseURL
	} else if gitBaseURL != "" {
		result.BaseURL = gitBaseURL
	} else if file.BaseURL != "" {
		result.BaseURL = file.BaseURL
	}

	// Model resolution precedence:
	// 1. CLI flag (--model)
	// 2. git config --local git-agent.model
	// 3. YAML config file (~/.config/git-agent/config.yml)
	//
	// Agent-session environment variables (PI_MODEL, CLAUDE_CODE_MODEL,
	// CODEX_MODEL, MODEL, ...) are deliberately NOT part of this chain: a
	// session-injected model must not silently override the endpoint model the
	// user configured (flag / local git config / user config), which would send
	// unexpected model names to the user's endpoint.
	if flags.Model != "" {
		result.Model = flags.Model
	} else if gitModel != "" {
		result.Model = gitModel
	} else if file.Model != "" {
		result.Model = file.Model
	}
	result.SessionModel = getSessionModelEnv()
	result.CloudflareAIGatewayID = file.CloudflareAIGatewayID

	result.RequireGitAgentCoAuthor = flags.RequireGitAgentCoAuthor || file.RequireGitAgentCoAuthor
	result.RequireModelCoAuthor = flags.RequireModelCoAuthor || file.RequireModelCoAuthor
	// AutoModelCoAuthor: flags take precedence, then file, then nil (default true)
	if flags.AutoModelCoAuthor != nil {
		result.AutoModelCoAuthor = flags.AutoModelCoAuthor
	} else if file.AutoModelCoAuthor != nil {
		result.AutoModelCoAuthor = file.AutoModelCoAuthor
	}

	// The System One layer is optional: an empty key leaves it off, and the
	// CLI then makes every decision with the generative provider as before.
	// The base URL and model stay empty here so the defaults live in one place,
	// the TypeSafe client.
	result.JevAPIKey = firstNonEmpty(flags.JevAPIKey, file.JevAPIKey, os.Getenv("TYPESAFE_API_KEY"))
	result.JevBaseURL = firstNonEmpty(flags.JevBaseURL, file.JevBaseURL)
	result.JevModel = firstNonEmpty(flags.JevModel, file.JevModel)
	result.JevMode = firstNonEmpty(flags.JevMode, file.JevMode)
	result.JevMinConfidence = firstPositive(flags.JevMinConfidence, file.JevMinConfidence)
	result.JevMaxCalls = firstPositive(flags.JevMaxCalls, file.JevMaxCalls)

	result.RequestTimeout = resolveDuration(flags.RequestTimeout, file.RequestTimeout, DefaultRequestTimeout)
	result.HeartbeatInterval = resolveDuration(flags.HeartbeatInterval, file.HeartbeatInterval, DefaultHeartbeatInterval)

	return result, nil
}

// getSessionModelEnv reads the active AI agent session model for Co-Authored-By
// attribution (the model that produced the change), in priority order:
// pi > Claude Code / Anthropic > Codex / OpenAI.
// It is intentionally separate from the inference routing model. The generic
// MODEL variable is deliberately excluded: shells, CI, and unrelated tooling
// set it freely, so it cannot reliably identify the producing agent.
func getSessionModelEnv() string {
	envs := []string{
		"PI_MODEL",
		"CLAUDE_CODE_MODEL",
		"CLAUDE_MODEL",
		"ANTHROPIC_MODEL",
		"CODEX_MODEL",
		"OPENAI_MODEL",
	}
	for _, env := range envs {
		if val := strings.TrimSpace(os.Getenv(env)); val != "" {
			return val
		}
	}
	return ""
}

// firstNonEmpty returns the first value that holds something other than
// whitespace.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// firstPositive applies flag > file > zero, ignoring a non-positive value.
func firstPositive[T ~float64 | ~int](flag, fileValue T) T {
	if flag > 0 {
		return flag
	}
	if fileValue > 0 {
		return fileValue
	}
	return 0
}

// resolveDuration applies the precedence chain flag > file YAML > default,
// silently falling back to the default when the file value is unparseable.
func resolveDuration(flag time.Duration, fileValue string, def time.Duration) time.Duration {
	if flag > 0 {
		return flag
	}
	if fileValue != "" {
		if d, err := time.ParseDuration(fileValue); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// ResolveField resolves a single config key across all scopes and reports which
// scope the value came from. Returns ("", "", nil) when the key is not set anywhere.
func ResolveField(ctx context.Context, repoRoot, userConfigPath, key string) (value, scope string, err error) {
	def, ok := KeyRegistry[key]
	if !ok {
		return "", "", fmt.Errorf("unknown config key %q", key)
	}

	// Provider-only keys live exclusively in user scope.
	if def.AllowUser && !def.AllowProject && !def.AllowLocal {
		v, found, e := ReadUserField(userConfigPath, key)
		if e != nil || !found {
			return "", "", e
		}
		return v, ScopeUser, nil
	}

	// Non-provider keys: local > project > user.
	if v, found, _ := ReadProjectField(LocalConfigPath(repoRoot), key); found {
		return v, ScopeLocal, nil
	}
	if v, found, _ := ReadProjectField(ProjectConfigPath(repoRoot), key); found {
		return v, ScopeProject, nil
	}
	if def.AllowUser {
		if v, found, _ := ReadUserField(userConfigPath, key); found {
			return v, ScopeUser, nil
		}
	}
	return "", "", nil
}
