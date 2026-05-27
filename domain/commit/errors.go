package commit

import "errors"

// ErrPlannerBudgetExhausted is the sentinel returned (wrapped) by the
// infrastructure LLM client when token doubling would exceed the
// per-endpoint ceiling. Use errors.Is to test for it; use errors.As with
// *PlannerBudgetExhaustedError to read Model and Ceiling.
var ErrPlannerBudgetExhausted = errors.New("planner budget exhausted")

// PlannerBudgetExhaustedError carries the model name and ceiling that
// were hit, so the cmd layer can render an actionable diagnostic.
type PlannerBudgetExhaustedError struct {
	Model   string
	Ceiling int
}

func (e *PlannerBudgetExhaustedError) Error() string {
	return ErrPlannerBudgetExhausted.Error()
}

func (e *PlannerBudgetExhaustedError) Is(target error) bool {
	return target == ErrPlannerBudgetExhausted
}
