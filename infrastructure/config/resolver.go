package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultBaseURL = "https://api.anthropic.com/v1"
const DefaultModel = "claude-3-5-haiku-20241022"

// DefaultRequestTimeout bounds the per-HTTP-request deadline given to the LLM
// client, including streamed completions. Chosen to comfortably exceed a slow
// 10 KB/s response while still cutting the wire if the upstream stalls.
const DefaultRequestTimeout = 90 * time.Second

// DefaultHeartbeatInterval is the cadence at which the CLI emits "still
// waiting" progress lines while an LLM call is in flight.
const DefaultHeartbeatInterval = 15 * time.Second

// DefaultDiagnoseTimeout bounds the per-HTTP-request deadline for the graph
// diagnose LLM re-rank. The re-rank is a cold forensics path that may target a
// more capable (and slower) model than commit generation, so it gets a longer
// budget than DefaultRequestTimeout.
const DefaultDiagnoseTimeout = 120 * time.Second

// Build-time defaults injected via -ldflags "-X github.com/gitagenthq/git-agent/infrastructure/config.BuildAPIKey=..."
var (
	BuildAPIKey  = ""
	BuildBaseURL = ""
	BuildModel   = ""
)

type ProviderConfig struct {
	APIKey               string
	BaseURL              string
	Model                string
	RequestTimeout       time.Duration // 0 = use DefaultRequestTimeout
	HeartbeatInterval    time.Duration // 0 = use DefaultHeartbeatInterval
	FreeMode             bool          // When true, use only build-time proxy credentials; all user config sources are ignored
	NoGitAgentCoAuthor   bool          // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor      bool          // When true, ignore all --co-author trailers
	RequireModelCoAuthor bool          // When true, every commit must carry a Co-Authored-By from an AI-provider domain
	ModelCoAuthorDomains []string      // Extra email domains accepted by the require check; appended to project.DefaultModelCoAuthorDomains

	// Diagnose-* fields configure the optional `graph diagnose --llm` re-ranker.
	// Each falls back to the matching main field when unset, so a user who wants
	// a single smarter model sets only git-agent.diagnose-model.
	DiagnoseModel   string        // model for the re-rank LLM (default: Model)
	DiagnoseBaseURL string        // base URL for the re-rank LLM (default: BaseURL)
	DiagnoseAPIKey  string        // API key for the re-rank LLM (default: APIKey)
	DiagnoseTimeout time.Duration // per-attempt HTTP timeout (0 = DefaultDiagnoseTimeout)
}

type fileConfig struct {
	APIKey               string   `yaml:"api_key"`
	BaseURL              string   `yaml:"base_url"`
	Model                string   `yaml:"model"`
	RequestTimeout       string   `yaml:"request_timeout"`
	HeartbeatInterval    string   `yaml:"heartbeat_interval"`
	NoGitAgentCoAuthor   bool     `yaml:"no_git_agent_co_author"`
	NoModelCoAuthor      bool     `yaml:"no_model_co_author"`
	RequireModelCoAuthor bool     `yaml:"require_model_co_author"`
	ModelCoAuthorDomains []string `yaml:"model_co_author_domains"`

	DiagnoseModel   string `yaml:"diagnose_model"`
	DiagnoseBaseURL string `yaml:"diagnose_base_url"`
	DiagnoseAPIKey  string `yaml:"diagnose_api_key"`
	DiagnoseTimeout string `yaml:"diagnose_timeout"`
}

// Resolve merges config from (highest to lowest priority):
// CLI flags > git config --local git-agent.* > YAML file > build-time defaults > hardcoded defaults.
// When FreeMode is true, only build-time proxy credentials are used; all user config sources are ignored.
func Resolve(ctx context.Context, flags ProviderConfig, configPath string) (*ProviderConfig, error) {
	if flags.FreeMode {
		result := &ProviderConfig{
			APIKey:            BuildAPIKey,
			BaseURL:           BuildBaseURL,
			Model:             BuildModel,
			RequestTimeout:    flags.RequestTimeout,
			HeartbeatInterval: flags.HeartbeatInterval,
		}
		if result.BaseURL == "" {
			result.BaseURL = DefaultBaseURL
		}
		if result.Model == "" {
			result.Model = DefaultModel
		}
		if result.RequestTimeout <= 0 {
			result.RequestTimeout = DefaultRequestTimeout
		}
		if result.HeartbeatInterval <= 0 {
			result.HeartbeatInterval = DefaultHeartbeatInterval
		}
		// In free mode the diagnose re-ranker shares the build-time provider.
		result.DiagnoseModel = result.Model
		result.DiagnoseBaseURL = result.BaseURL
		result.DiagnoseAPIKey = result.APIKey
		result.DiagnoseTimeout = resolveDuration(flags.DiagnoseTimeout, "", DefaultDiagnoseTimeout)
		return result, nil
	}

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
		}
	}

	gitModel, _ := ReadGitConfig(ctx, "model")
	gitBaseURL, _ := ReadGitConfig(ctx, "base-url")
	gitDiagnoseModel, _ := ReadGitConfig(ctx, "diagnose-model")
	gitDiagnoseBaseURL, _ := ReadGitConfig(ctx, "diagnose-base-url")
	gitDiagnoseAPIKey, _ := ReadGitConfig(ctx, "diagnose-api-key")
	gitDiagnoseTimeout, _ := ReadGitConfig(ctx, "diagnose-timeout")

	result := &ProviderConfig{}

	if flags.APIKey != "" {
		result.APIKey = flags.APIKey
	} else if file.APIKey != "" {
		result.APIKey = file.APIKey
	} else if BuildAPIKey != "" {
		result.APIKey = BuildAPIKey
	}

	if flags.BaseURL != "" {
		result.BaseURL = flags.BaseURL
	} else if gitBaseURL != "" {
		result.BaseURL = gitBaseURL
	} else if file.BaseURL != "" {
		result.BaseURL = file.BaseURL
	} else if BuildBaseURL != "" {
		result.BaseURL = BuildBaseURL
	} else {
		result.BaseURL = DefaultBaseURL
	}

	if flags.Model != "" {
		result.Model = flags.Model
	} else if gitModel != "" {
		result.Model = gitModel
	} else if file.Model != "" {
		result.Model = file.Model
	} else if BuildModel != "" {
		result.Model = BuildModel
	} else {
		result.Model = DefaultModel
	}

	result.NoGitAgentCoAuthor = flags.NoGitAgentCoAuthor || file.NoGitAgentCoAuthor
	result.NoModelCoAuthor = flags.NoModelCoAuthor || file.NoModelCoAuthor
	result.RequireModelCoAuthor = flags.RequireModelCoAuthor || file.RequireModelCoAuthor

	if len(flags.ModelCoAuthorDomains) > 0 {
		result.ModelCoAuthorDomains = append(result.ModelCoAuthorDomains, flags.ModelCoAuthorDomains...)
	}
	if len(file.ModelCoAuthorDomains) > 0 {
		result.ModelCoAuthorDomains = append(result.ModelCoAuthorDomains, file.ModelCoAuthorDomains...)
	}

	result.RequestTimeout = resolveDuration(flags.RequestTimeout, file.RequestTimeout, DefaultRequestTimeout)
	result.HeartbeatInterval = resolveDuration(flags.HeartbeatInterval, file.HeartbeatInterval, DefaultHeartbeatInterval)

	// Diagnose re-ranker fields: flag > git config > YAML file > fall back to the
	// matching main field (so a user who wants one smarter model sets only
	// git-agent.diagnose-model). Timeout defaults to DefaultDiagnoseTimeout.
	result.DiagnoseModel = firstNonEmpty(flags.DiagnoseModel, gitDiagnoseModel, file.DiagnoseModel, result.Model)
	result.DiagnoseBaseURL = firstNonEmpty(flags.DiagnoseBaseURL, gitDiagnoseBaseURL, file.DiagnoseBaseURL, result.BaseURL)
	result.DiagnoseAPIKey = firstNonEmpty(flags.DiagnoseAPIKey, gitDiagnoseAPIKey, file.DiagnoseAPIKey, result.APIKey)
	result.DiagnoseTimeout = resolveDiagnoseTimeout(flags.DiagnoseTimeout, gitDiagnoseTimeout, file.DiagnoseTimeout, DefaultDiagnoseTimeout)

	return result, nil
}

// firstNonEmpty returns the first non-empty argument, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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

// resolveDiagnoseTimeout applies flag > git-config > YAML file > default for the
// diagnose re-rank timeout. git-config and YAML are strings (e.g. "120s").
func resolveDiagnoseTimeout(flag time.Duration, gitValue, fileValue string, def time.Duration) time.Duration {
	if flag > 0 {
		return flag
	}
	if d, ok := parseDurationNonEmpty(gitValue); ok {
		return d
	}
	if d, ok := parseDurationNonEmpty(fileValue); ok {
		return d
	}
	return def
}

// parseDurationNonEmpty parses a duration string, returning (zero, false) on
// empty or unparseable input.
func parseDurationNonEmpty(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
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
