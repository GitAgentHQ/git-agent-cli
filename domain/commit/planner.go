package commit

import (
	"context"

	"github.com/gitagenthq/git-agent/domain/diff"
	"github.com/gitagenthq/git-agent/domain/project"
)

// CommitPlanner analyses a set of changes and proposes how to split them
// into atomic commits.
type CommitPlanner interface {
	Plan(ctx context.Context, req PlanRequest) (*CommitPlan, error)
}

// CoChangeHint describes a co-change relationship between two files,
// used to inform commit grouping decisions.
type CoChangeHint struct {
	FileA    string
	FileB    string
	Strength float64 // 0.0-1.0
	// Subjects are the first lines of a few commits that historically changed
	// FileA and FileB together — the semantic reason the pair is coupled, not
	// just the count. Empty when no linking commit is available.
	Subjects []string
}

// PlanRequest carries all context needed to produce a CommitPlan.
type PlanRequest struct {
	StagedDiff    *diff.StagedDiff
	UnstagedDiff  *diff.StagedDiff
	Intent        string
	Config        *project.Config
	CoChangeHints []CoChangeHint
}
