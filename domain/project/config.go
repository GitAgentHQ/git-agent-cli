package project

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes             []string
	Hooks              []string // ordered list: "conventional", file paths, etc. Empty = no validation.
	MaxDiffLines       int      // 0 = no limit
	NoGitAgentCoAuthor bool     // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor    bool     // When true, ignore all --co-author trailers
}
