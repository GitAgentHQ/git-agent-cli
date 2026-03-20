package project

// Config holds project-level configuration for git-agent.
type Config struct {
	Scopes      []string
	HookType    string // "conventional", "empty", or path to script
}
