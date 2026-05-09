package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gitagenthq/git-agent/domain/project"
)

// ProjectConfigPath returns the path to read project-scope config.
// Prefers .git-agent/config.yml; falls back to .git-agent/project.yml for
// backward compatibility. Returns .git-agent/config.yml when neither exists.
func ProjectConfigPath(repoRoot string) string {
	newPath := filepath.Join(repoRoot, ".git-agent", "config.yml")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	legacyPath := filepath.Join(repoRoot, ".git-agent", "project.yml")
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	return newPath
}

// ProjectConfigWritePath returns the canonical write path for project-scope config.
func ProjectConfigWritePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".git-agent", "config.yml")
}

// LocalConfigPath returns the path for local-scope config (not checked into git).
func LocalConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".git-agent", "config.local.yml")
}

// rawScope supports both legacy string format and structured format in YAML.
type rawScope struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// UnmarshalYAML allows a scope to be either a plain string or a map with name/description.
func (s *rawScope) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		s.Name = value.Value
		return nil
	}
	type plain rawScope
	return value.Decode((*plain)(s))
}

// rawProjectConfig is the YAML shape for project/local config files.
type rawProjectConfig struct {
	Scopes               []rawScope `yaml:"scopes,omitempty"`
	Hooks                []string   `yaml:"hook,omitempty"`
	HookTypeLegacy       string     `yaml:"hook_type,omitempty"` // backward compat: migrated to hook on load
	MaxDiffLines         *int       `yaml:"max_diff_lines,omitempty"`
	NoGitAgentCoAuthor   *bool      `yaml:"no_git_agent_co_author,omitempty"`
	NoModelCoAuthor      *bool      `yaml:"no_model_co_author,omitempty"`
	RequireModelCoAuthor *bool      `yaml:"require_model_co_author,omitempty"`
	ModelCoAuthorDomains []string   `yaml:"model_co_author_domains,omitempty"`
}

func loadRawProjectConfig(path string) rawProjectConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return rawProjectConfig{}
	}
	var raw rawProjectConfig
	_ = yaml.Unmarshal(data, &raw)
	return raw
}

// migrateHooks returns the effective hooks slice, migrating legacy hook_type to hook array.
func migrateHooks(raw rawProjectConfig) []string {
	if len(raw.Hooks) > 0 {
		return raw.Hooks
	}
	if raw.HookTypeLegacy != "" {
		return []string{raw.HookTypeLegacy}
	}
	return nil
}

// LoadProjectConfig loads and merges local > project > user config into a domain Config.
// Returns nil when no config files exist.
func LoadProjectConfig(repoRoot, userConfigPath string) *project.Config {
	proj := loadRawProjectConfig(ProjectConfigPath(repoRoot))
	local := loadRawProjectConfig(LocalConfigPath(repoRoot))
	user := loadRawProjectConfig(userConfigPath)

	merged := proj
	merged.Hooks = migrateHooks(proj)
	merged.HookTypeLegacy = ""

	// scopes: local > project > user
	if len(local.Scopes) > 0 {
		merged.Scopes = local.Scopes
	} else if len(merged.Scopes) == 0 && len(user.Scopes) > 0 {
		merged.Scopes = user.Scopes
	}
	// hooks: local > project > user
	localHooks := migrateHooks(local)
	if len(localHooks) > 0 {
		merged.Hooks = localHooks
	} else if len(merged.Hooks) == 0 {
		userHooks := migrateHooks(user)
		if len(userHooks) > 0 {
			merged.Hooks = userHooks
		}
	}
	if local.MaxDiffLines != nil {
		merged.MaxDiffLines = local.MaxDiffLines
	}
	if local.NoGitAgentCoAuthor != nil {
		merged.NoGitAgentCoAuthor = local.NoGitAgentCoAuthor
	}
	if local.NoModelCoAuthor != nil {
		merged.NoModelCoAuthor = local.NoModelCoAuthor
	}
	if local.RequireModelCoAuthor != nil {
		merged.RequireModelCoAuthor = local.RequireModelCoAuthor
	}
	if len(local.ModelCoAuthorDomains) > 0 {
		merged.ModelCoAuthorDomains = local.ModelCoAuthorDomains
	}

	if len(merged.Scopes) == 0 && len(merged.Hooks) == 0 && merged.MaxDiffLines == nil &&
		merged.NoGitAgentCoAuthor == nil && merged.NoModelCoAuthor == nil &&
		merged.RequireModelCoAuthor == nil && len(merged.ModelCoAuthorDomains) == 0 {
		return nil
	}

	scopes := make([]project.Scope, len(merged.Scopes))
	for i, s := range merged.Scopes {
		scopes[i] = project.Scope{Name: s.Name, Description: s.Description}
	}

	cfg := &project.Config{
		Scopes: scopes,
		Hooks:  merged.Hooks,
	}
	if merged.MaxDiffLines != nil {
		cfg.MaxDiffLines = *merged.MaxDiffLines
	}
	if merged.NoGitAgentCoAuthor != nil {
		cfg.NoGitAgentCoAuthor = *merged.NoGitAgentCoAuthor
	}
	if merged.NoModelCoAuthor != nil {
		cfg.NoModelCoAuthor = *merged.NoModelCoAuthor
	}
	if merged.RequireModelCoAuthor != nil {
		cfg.RequireModelCoAuthor = *merged.RequireModelCoAuthor
	}
	if len(merged.ModelCoAuthorDomains) > 0 {
		cfg.ModelCoAuthorDomains = append([]string(nil), merged.ModelCoAuthorDomains...)
	}
	return cfg
}

// WriteProjectField writes a key-value pair to the given config file,
// preserving all existing keys.
func WriteProjectField(path, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	rawMap := ReadYAMLMap(path)
	rawMap[key] = coerceForWrite(key, value)
	data, err := yaml.Marshal(rawMap)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ReadProjectField reads a single key from a specific config file.
// Returns ("", false, nil) when the key is not present.
func ReadProjectField(path, key string) (string, bool, error) {
	rawMap := ReadYAMLMap(path)
	v, ok := rawMap[key]
	if !ok {
		return "", false, nil
	}
	return yamlValueToString(v), true, nil
}

func ReadYAMLMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]any)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil || m == nil {
		return make(map[string]any)
	}
	return m
}

func yamlValueToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case []any:
		var parts []string
		for _, item := range val {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", val)
	}
}
