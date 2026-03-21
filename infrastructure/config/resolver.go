package config

import (
	"context"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultBaseURL = "https://api.anthropic.com/v1"
const DefaultModel = "claude-3-5-haiku-20241022"

// Build-time defaults injected via -ldflags "-X github.com/fradser/git-agent/infrastructure/config.BuildAPIKey=..."
var (
	BuildAPIKey  = ""
	BuildBaseURL = ""
	BuildModel   = ""
)

type ProviderConfig struct {
	APIKey   string
	BaseURL  string
	Model    string
	FreeMode bool // When true, ignore config file, git config, and build-time defaults
}

type fileConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

// Resolve merges config from (highest to lowest priority):
// CLI flags > git config --local git-agent.* > YAML file > build-time defaults > hardcoded defaults.
// When FreeMode is true, ignore git config, YAML file, and build-time defaults.
func Resolve(ctx context.Context, flags ProviderConfig, configPath string) (*ProviderConfig, error) {
	var file fileConfig

	if configPath != "" && !flags.FreeMode {
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

	var gitModel, gitBaseURL string
	if !flags.FreeMode {
		gitModel, _ = ReadGitConfig(ctx, "model")
		gitBaseURL, _ = ReadGitConfig(ctx, "base-url")
	}

	result := &ProviderConfig{}

	if flags.APIKey != "" {
		result.APIKey = flags.APIKey
	} else if file.APIKey != "" && !flags.FreeMode {
		result.APIKey = file.APIKey
	} else if BuildAPIKey != "" && !flags.FreeMode {
		result.APIKey = BuildAPIKey
	}

	if flags.BaseURL != "" {
		result.BaseURL = flags.BaseURL
	} else if gitBaseURL != "" && !flags.FreeMode {
		result.BaseURL = gitBaseURL
	} else if file.BaseURL != "" && !flags.FreeMode {
		result.BaseURL = file.BaseURL
	} else if BuildBaseURL != "" && !flags.FreeMode {
		result.BaseURL = BuildBaseURL
	} else {
		result.BaseURL = DefaultBaseURL
	}

	if flags.Model != "" {
		result.Model = flags.Model
	} else if gitModel != "" && !flags.FreeMode {
		result.Model = gitModel
	} else if file.Model != "" && !flags.FreeMode {
		result.Model = file.Model
	} else if BuildModel != "" && !flags.FreeMode {
		result.Model = BuildModel
	} else {
		result.Model = DefaultModel
	}

	return result, nil
}
