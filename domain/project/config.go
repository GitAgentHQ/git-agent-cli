package project

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes        []string
	HookType      string // "conventional", "empty", or path to script
	NoGitAgentCoAuthor bool // When true, omit the default Co-Authored-By: Git Agent trailer
	NoModelCoAuthor    bool   // When true, ignore all --co-author trailers
}
