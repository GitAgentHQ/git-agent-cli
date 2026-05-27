package commit_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gitagenthq/git-agent/domain/commit"
)

func TestPlannerBudgetExhausted_ErrorsIs(t *testing.T) {
	err := &commit.PlannerBudgetExhaustedError{Model: "test-model", Ceiling: 16384}
	if !errors.Is(err, commit.ErrPlannerBudgetExhausted) {
		t.Errorf("errors.Is(carrier, ErrPlannerBudgetExhausted) = false, want true")
	}
}

func TestPlannerBudgetExhausted_ErrorMessage(t *testing.T) {
	err := &commit.PlannerBudgetExhaustedError{Model: "X", Ceiling: 16384}
	const want = "planner budget exhausted"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestPlannerBudgetExhausted_ErrorsAs_AfterWrap(t *testing.T) {
	wrapped := fmt.Errorf("plan commits: %w", &commit.PlannerBudgetExhaustedError{
		Model:   "M",
		Ceiling: 16384,
	})

	var target *commit.PlannerBudgetExhaustedError
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As did not extract *PlannerBudgetExhaustedError from %v", wrapped)
	}
	if target.Model != "M" {
		t.Errorf("target.Model = %q, want %q", target.Model, "M")
	}
	if target.Ceiling != 16384 {
		t.Errorf("target.Ceiling = %d, want %d", target.Ceiling, 16384)
	}
}
