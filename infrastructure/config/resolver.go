package config

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultBaseURL = "https://api.anthropic.com/v1"
const DefaultModel = "claude-3-5-haiku-20241022"

// Build-time defaults injected via -ldflags "-X github.com/gitagenthq/git-agent/infrastructure/config.BuildAPIKey=..."
var (
	BuildAPIKey  = ""
	BuildBaseURL = ""
	BuildModel   = ""
)

type ProviderConfig struct {
	APIKey        string
	BaseURL       string
	Model         string
	FreeMode      bool // When true, use only build-time proxy credentials; all user config sources are ignored
	NoGitAgentCoAuthor bool // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor         bool // When true, ignore all --co-author trailers
}

type fileConfig struct {
	APIKey        string `yaml:"api_key"`
	BaseURL       string `yaml:"base_url"`
	Model         string `yaml:"model"`
	NoGitAgentCoAuthor bool `yaml:"no_git_agent_co_author"`
	NoModelCoAuthor         bool `yaml:"no_model_co_author"`
}

// Resolve merges config from (highest to lowest priority):
// CLI flags > git config --local git-agent.* > YAML file > build-time defaults > hardcoded defaults.
// When FreeMode is true, only build-time proxy credentials are used; all user config sources are ignored.
func Resolve(ctx context.Context, flags ProviderConfig, configPath string) (*ProviderConfig, error) {
	if flags.FreeMode {
		result := &ProviderConfig{
			APIKey:  BuildAPIKey,
			BaseURL: BuildBaseURL,
			Model:   BuildModel,
		}
		if result.BaseURL == "" {
			result.BaseURL = DefaultBaseURL
		}
		if result.Model == "" {
			result.Model = DefaultModel
		}
		return result, nil
	}

	var file fileConfig
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			// Silently ignore parse errors — treat as empty config.
			_ = yaml.Unmarshal(data, &file)
			file.APIKey = os.ExpandEnv(file.APIKey)
			file.BaseURL = os.ExpandEnv(file.BaseURL)
			file.Model = os.ExpandEnv(file.Model)
		}
	}

	gitModel, _ := ReadGitConfig(ctx, "model")
	gitBaseURL, _ := ReadGitConfig(ctx, "base-url")

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

	return result, nil
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
