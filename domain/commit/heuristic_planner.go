package commit

import "context"

// HeuristicPlanner builds a CommitPlan without consulting an LLM. It is the
// REQ-008 safety net: when the LLM planner exhausts its token budget on a
// large change set, the application layer can fall back to a deterministic
// planner so the commit still gets made.
//
// Implementations must not perform network or process IO; they exist to keep
// the commit pipeline moving when the primary planner has given up.
type HeuristicPlanner interface {
	Plan(ctx context.Context, req PlanRequest) (*CommitPlan, error)
}
