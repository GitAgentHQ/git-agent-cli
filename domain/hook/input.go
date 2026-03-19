package hook

import "github.com/fradser/ga-cli/domain/project"

// HookInput is the JSON payload passed to ga hooks.
type HookInput struct {
	Diff          string         `json:"diff"`
	CommitMessage string         `json:"commit_message"`
	Intent        string         `json:"intent"`
	StagedFiles   []string       `json:"staged_files"`
	Config        project.Config `json:"config"`
}
