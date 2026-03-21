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

// PlanRequest carries all context needed to produce a CommitPlan.
type PlanRequest struct {
	StagedDiff   *diff.StagedDiff
	UnstagedDiff *diff.StagedDiff
	Intent       string
	Config       *project.Config
}
