package project

import (
	"encoding/json"
	"strings"
)

// Scope represents a commit scope with an optional description to help AI
// understand the scope's purpose during commit message generation.
type Scope struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// UnmarshalJSON allows Scope to be decoded from either a plain string ("app")
// or a full object ({"name":"app","description":"..."}).
func (s *Scope) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		s.Name = name
		return nil
	}
	type alias Scope
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = Scope(a)
	return nil
}

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes               []Scope  `json:"scopes"`
	Hooks                []string `json:"hooks"`                // ordered list: "conventional", file paths, etc. Empty = no validation.
	MaxDiffLines         int      `json:"maxDiffLines"`         // 0 = no limit
	NoGitAgentCoAuthor   bool     `json:"noGitAgentCoAuthor"`   // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor      bool     `json:"noModelCoAuthor"`      // When true, ignore all --co-author trailers
	RequireModelCoAuthor bool     `json:"requireModelCoAuthor"` // When true, every commit must carry a Co-Authored-By from an AI-provider domain
	ModelCoAuthorDomains []string `json:"modelCoAuthorDomains"` // Extra email domains accepted by the require check; appended to DefaultModelCoAuthorDomains
}

// DefaultModelCoAuthorDomains is the built-in allow-list of email domains
// that count as a "model" co-author for RequireModelCoAuthor enforcement.
// User-supplied ModelCoAuthorDomains are appended to this list.
var DefaultModelCoAuthorDomains = []string{
	"anthropic.com",
	"openai.com",
	"google.com",
}

// ScopeNames returns just the scope name strings.
func (c *Config) ScopeNames() []string {
	names := make([]string, len(c.Scopes))
	for i, s := range c.Scopes {
		names[i] = s.Name
	}
	return names
}

// FormatScopesForLLM returns a human-readable scope list for LLM prompts.
// Scopes with descriptions are formatted as "name: description"; plain scopes
// are listed by name only.
func (c *Config) FormatScopesForLLM() string {
	parts := make([]string, len(c.Scopes))
	for i, s := range c.Scopes {
		if s.Description != "" {
			parts[i] = s.Name + " — " + s.Description
		} else {
			parts[i] = s.Name
		}
	}
	return strings.Join(parts, "\n- ")
}
