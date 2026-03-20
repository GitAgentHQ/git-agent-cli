package hook

import "github.com/fradser/git-agent/domain/project"

// HookInput is the JSON payload passed to git-agent hooks.
type HookInput struct {
	Diff          string         `json:"diff"`
	CommitMessage string         `json:"commit_message"`
	Intent        string         `json:"intent"`
	StagedFiles   []string       `json:"staged_files"`
	Config        project.Config `json:"config"`
}
