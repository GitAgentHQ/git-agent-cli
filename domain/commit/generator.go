package commit

import (
	"context"

	"github.com/fradser/git-agent/domain/diff"
	"github.com/fradser/git-agent/domain/project"
)

// GenerateRequest contains everything needed to generate a commit message.
type GenerateRequest struct {
	Diff         *diff.StagedDiff
	Intent       string
	Config       *project.Config
	Verbose      bool
	// HookFeedback carries the rejection reason from a previous hook block,
	// so the LLM can correct the message on retry.
	HookFeedback string
}

// CommitMessageGenerator generates commit messages from staged diffs.
type CommitMessageGenerator interface {
	Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}
