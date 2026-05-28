package commit

import (
	"errors"
	"time"
)

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

// ErrPlannerTimedOut is the sentinel returned (wrapped) by the infrastructure
// LLM client when the per-attempt request deadline elapses. The client returns
// this on the FIRST timeout (no retries) because retrying with the same
// timeout will produce the same outcome — the model needed >timeout to
// respond, and a second attempt will not change that. Use errors.Is to test
// for it; use errors.As with *PlannerTimedOutError to read Model and Timeout.
var ErrPlannerTimedOut = errors.New("planner timed out")

// PlannerTimedOutError carries the model name and configured per-attempt
// timeout that elapsed, so the cmd layer can render an actionable diagnostic
// naming both.
type PlannerTimedOutError struct {
	Model   string
	Timeout time.Duration
}

func (e *PlannerTimedOutError) Error() string {
	return ErrPlannerTimedOut.Error()
}

func (e *PlannerTimedOutError) Is(target error) bool {
	return target == ErrPlannerTimedOut
}
