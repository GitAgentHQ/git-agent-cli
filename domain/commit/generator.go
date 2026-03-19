package commit

import (
	"context"

	"github.com/fradser/ga-cli/domain/diff"
	"github.com/fradser/ga-cli/domain/project"
)

// GenerateRequest contains everything needed to generate a commit message.
type GenerateRequest struct {
	Diff    *diff.StagedDiff
	Intent  string
	Config  *project.Config
	Verbose bool
}

// CommitMessageGenerator generates commit messages from staged diffs.
type CommitMessageGenerator interface {
	Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}
