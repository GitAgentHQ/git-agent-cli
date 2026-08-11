package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// BuildBaseURL is the shared free-gateway URL embedded at build time via
// -ldflags "-X .../config.BuildBaseURL=...". It is a URL only — never a
// credential. Official release binaries point here so a user with zero
// configuration gets the free shared gateway; any user-provided base_url
// overrides it. Empty for dev builds (no built-in default).
var BuildBaseURL = ""

// DefaultRequestTimeout bounds the per-HTTP-request deadline given to the LLM
// client, including streamed completions. Chosen to comfortably exceed a slow
// 10 KB/s response while still cutting the wire if the upstream stalls.
const DefaultRequestTimeout = 90 * time.Second

// DefaultHeartbeatInterval is the cadence at which the CLI emits "still
// waiting" progress lines while an LLM call is in flight.
const DefaultHeartbeatInterval = 15 * time.Second

type ProviderConfig struct {
	APIKey                string
	BaseURL               string
	Model                 string
	CloudflareAIGatewayID string
	RequestTimeout        time.Duration // 0 = use DefaultRequestTimeout
	HeartbeatInterval     time.Duration // 0 = use DefaultHeartbeatInterval
	ForceFreeGateway      bool          // When true, route via the free shared gateway (--free), overriding api_key/base_url/model
	NoGitAgentCoAuthor    bool          // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor       bool          // When true, ignore all --co-author trailers
	RequireModelCoAuthor  bool          // When true, every commit must carry a Co-Authored-By from an AI-provider domain
	ModelCoAuthorDomains  []string      // Extra email domains accepted by the require check; appended to project.DefaultModelCoAuthorDomains
}

type fileConfig struct {
	APIKey                string   `yaml:"api_key"`
	BaseURL               string   `yaml:"base_url"`
	Model                 string   `yaml:"model"`
	CloudflareAIGatewayID string   `yaml:"cloudflare_ai_gateway_id"`
	RequestTimeout        string   `yaml:"request_timeout"`
	HeartbeatInterval     string   `yaml:"heartbeat_interval"`
	NoGitAgentCoAuthor    bool     `yaml:"no_git_agent_co_author"`
	NoModelCoAuthor       bool     `yaml:"no_model_co_author"`
	RequireModelCoAuthor  bool     `yaml:"require_model_co_author"`
	ModelCoAuthorDomains  []string `yaml:"model_co_author_domains"`
}

// Resolve merges config from (highest to lowest priority):
// CLI flags > git config --local git-agent.* > YAML file > build-time default
// (BuildBaseURL, free shared gateway) > empty.
//
// ForceFreeGateway (--free) overrides the merge for the routing fields only:
// the request goes to the embedded shared gateway regardless of any user
// api_key / base_url / model. Non-routing fields (timeouts, co-author policy)
// still resolve normally.
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
	} else if BuildBaseURL != "" {
		result.BaseURL = BuildBaseURL
	}

	if flags.Model != "" {
		result.Model = flags.Model
	} else if gitModel != "" {
		result.Model = gitModel
	} else if file.Model != "" {
		result.Model = file.Model
	} else if piModel := os.Getenv("PI_MODEL"); piModel != "" {
		result.Model = piModel
	} else if envModel := os.Getenv("MODEL"); envModel != "" {
		result.Model = envModel
	}
	result.CloudflareAIGatewayID = file.CloudflareAIGatewayID

	// --free forces routing through the embedded shared gateway: clear api_key
	// (the Worker holds the credential server-side), pin base_url to the
	// build-time gateway URL, and let the Worker pick the model. BuildBaseURL is
	// empty for dev builds, so the caller's providerConfigError then surfaces the
	// "install an official release binary" hint.
	if flags.ForceFreeGateway {
		result.APIKey = ""
		result.BaseURL = BuildBaseURL
		result.Model = ""
		result.CloudflareAIGatewayID = ""
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

	return result, nil
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
