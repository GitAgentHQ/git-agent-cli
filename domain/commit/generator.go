package commit

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

// GenerateRequest contains everything needed to generate a commit message.
type GenerateRequest struct {
	Diff   *diff.StagedDiff
	Intent string
	Config *project.Config
	// Language overrides Config.Language when set; empty means use config/auto.
	Language string
	Verbose  bool
	// HookFeedback carries the rejection reason from a previous hook block,
	// so the LLM can correct the message on retry.
	HookFeedback string
	// PreviousMessage carries the full assembled commit message from the prior
	// attempt. When set alongside HookFeedback, the generator reformats this
	// message instead of re-analyzing the diff.
	PreviousMessage string
	// DecidedPrefix pins the conventional-commit prefix of the title, such as
	// "fix(app)". A judgment layer sets it after it chose the type and scope;
	// the generator then writes only the description that follows. An empty
	// value leaves the whole title to the generator.
	DecidedPrefix string
	// PinnedScope restricts the title to one scope without fixing the type: the
	// scope is the part a judgment measures as reliable enough to pin. An empty
	// value leaves the scope to the generator.
	PinnedScope string
}

// CommitMessageGenerator generates commit messages from staged diffs.
type CommitMessageGenerator interface {
	Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}
