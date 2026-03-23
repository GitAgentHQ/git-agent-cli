package project

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes             []string `json:"scopes"`
	Hooks              []string `json:"hooks"`              // ordered list: "conventional", file paths, etc. Empty = no validation.
	MaxDiffLines       int      `json:"maxDiffLines"`       // 0 = no limit
	NoGitAgentCoAuthor bool     `json:"noGitAgentCoAuthor"` // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor    bool     `json:"noModelCoAuthor"`    // When true, ignore all --co-author trailers
}
