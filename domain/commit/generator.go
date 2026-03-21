package commit

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
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
	// PreviousMessage carries the full assembled commit message from the prior
	// attempt. When set alongside HookFeedback, the generator reformats this
	// message instead of re-analyzing the diff.
	PreviousMessage string
}

// CommitMessageGenerator generates commit messages from staged diffs.
type CommitMessageGenerator interface {
	Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}
