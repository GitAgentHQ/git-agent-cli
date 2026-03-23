package hook

import "github.com/gitagenthq/git-agent/domain/project"

// HookInput is the JSON payload passed to git-agent hooks.
type HookInput struct {
	Diff          string         `json:"diff"`
	CommitMessage string         `json:"commitMessage"`
	Intent        string         `json:"intent"`
	StagedFiles   []string       `json:"stagedFiles"`
	Config        project.Config `json:"config"`
}
