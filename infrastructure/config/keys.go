package config

import (
	"fmt"
	"strconv"
	"strings"
)


const (
	ScopeUser    = "user"
	ScopeProject = "project"
	ScopeLocal   = "local"
)

// KeyDef describes a config key's type and which scopes allow it.
type KeyDef struct {
	Name         string
	Type         string // "string", "bool", "stringslice", "int"
	AllowUser    bool
	AllowProject bool
	AllowLocal   bool
}

// KeyRegistry is the authoritative list of supported config keys.
var KeyRegistry = map[string]KeyDef{
	"api_key":                {Name: "api_key", Type: "string", AllowUser: true},
	"base_url":               {Name: "base_url", Type: "string", AllowUser: true},
	"model":                  {Name: "model", Type: "string", AllowUser: true},
	"scopes":                 {Name: "scopes", Type: "stringslice", AllowProject: true, AllowLocal: true},
	"hook":                   {Name: "hook", Type: "stringslice", AllowProject: true, AllowLocal: true},
	"max_diff_lines":         {Name: "max_diff_lines", Type: "int", AllowProject: true, AllowLocal: true},
	"no_git_agent_co_author": {Name: "no_git_agent_co_author", Type: "bool", AllowUser: true, AllowProject: true, AllowLocal: true},
	"no_model_co_author":     {Name: "no_model_co_author", Type: "bool", AllowUser: true, AllowProject: true, AllowLocal: true},
}

// KeyAliases maps kebab-case flag names to their canonical snake_case registry keys.
var KeyAliases = map[string]string{
	"api-key":                "api_key",
	"base-url":               "base_url",
	"max-diff-lines":         "max_diff_lines",
	"no-git-agent-co-author": "no_git_agent_co_author",
	"no-model-co-author":     "no_model_co_author",
}

// ResolveKey normalizes a user-supplied key (kebab or snake) to its canonical
// snake_case name, or returns an error if the key is unknown.
func ResolveKey(raw string) (string, error) {
	if _, ok := KeyRegistry[raw]; ok {
		return raw, nil
	}
	if canonical, ok := KeyAliases[raw]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("unknown config key %q", raw)
}

// ValidateScope returns an error if key cannot be set in the given scope.
func ValidateScope(key, scope string) error {
	def, ok := KeyRegistry[key]
	if !ok {
		return fmt.Errorf("unknown config key %q", key)
	}
	switch scope {
	case ScopeUser:
		if !def.AllowUser {
			return fmt.Errorf("key %q cannot be set in user scope", key)
		}
	case ScopeProject:
		if !def.AllowProject {
			return fmt.Errorf("key %q cannot be set in project scope (provider keys belong in user scope)", key)
		}
	case ScopeLocal:
		if !def.AllowLocal {
			return fmt.Errorf("key %q cannot be set in local scope (provider keys belong in user scope)", key)
		}
	default:
		return fmt.Errorf("unknown scope %q: must be user, project, or local", scope)
	}
	return nil
}

// DefaultScope returns the default scope for a key when none is specified.
func DefaultScope(key string) string {
	def, ok := KeyRegistry[key]
	if !ok {
		return ScopeProject
	}
	if def.AllowUser && !def.AllowProject && !def.AllowLocal {
		return ScopeUser
	}
	return ScopeProject
}

// NormalizeValue validates and normalizes a raw string value for the given key.
func NormalizeValue(key, raw string) (string, error) {
	def, ok := KeyRegistry[key]
	if !ok {
		return "", fmt.Errorf("unknown config key %q", key)
	}
	switch def.Type {
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("invalid boolean value %q for %q: must be true or false", raw, key)
		}
		return strconv.FormatBool(b), nil
	case "int":
		n, err := strconv.Atoi(raw)
		if err != nil {
			return "", fmt.Errorf("invalid integer value %q for %q", raw, key)
		}
		return strconv.Itoa(n), nil
	case "stringslice":
		parts := strings.Split(raw, ",")
		var normalized []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				normalized = append(normalized, p)
			}
		}
		if len(normalized) == 0 {
			return "", fmt.Errorf("empty value for key %q", key)
		}
		return strings.Join(normalized, ","), nil
	default:
		if raw == "" {
			return "", fmt.Errorf("empty value for key %q", key)
		}
		return raw, nil
	}
}
