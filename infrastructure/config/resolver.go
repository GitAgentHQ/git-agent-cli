package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultBaseURL = "https://api.anthropic.com/v1"
const DefaultModel = "claude-3-5-haiku-20241022"

type ProviderConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type fileConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

func Resolve(flags ProviderConfig, configPath string) (*ProviderConfig, error) {
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

	result := &ProviderConfig{}

	if flags.APIKey != "" {
		result.APIKey = flags.APIKey
	} else {
		result.APIKey = file.APIKey
	}

	if flags.BaseURL != "" {
		result.BaseURL = flags.BaseURL
	} else if file.BaseURL != "" {
		result.BaseURL = file.BaseURL
	} else {
		result.BaseURL = DefaultBaseURL
	}

	if flags.Model != "" {
		result.Model = flags.Model
	} else if file.Model != "" {
		result.Model = file.Model
	} else {
		result.Model = DefaultModel
	}

	return result, nil
}
