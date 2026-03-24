package project

import "strings"

// Scope represents a commit scope with an optional description to help AI
// understand the scope's purpose during commit message generation.
type Scope struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes             []Scope  `json:"scopes"`
	Hooks              []string `json:"hooks"`              // ordered list: "conventional", file paths, etc. Empty = no validation.
	MaxDiffLines       int      `json:"maxDiffLines"`       // 0 = no limit
	NoGitAgentCoAuthor bool     `json:"noGitAgentCoAuthor"` // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor    bool     `json:"noModelCoAuthor"`    // When true, ignore all --co-author trailers
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
