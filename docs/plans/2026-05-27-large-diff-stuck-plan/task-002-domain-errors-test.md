# Task 002 — Domain errors (test)

| Field | Value |
|---|---|
| **subject** | Add unit tests for `commit.ErrPlannerBudgetExhausted` and `*commit.PlannerBudgetExhaustedError` |
| **type** | test |
| **depends-on** | [] |
| **REQ refs** | REQ-005, REQ-006 |
| **layer** | domain/commit |

## Files to create

- `domain/commit/errors_test.go`

## BDD Coverage

Foundation task — defines the typed error used by REQ-005 (token-ceiling abort) and REQ-006 (actionable diagnostic). The Scenario `Planner doubling halts at the ceiling after one attempted double` (`bdd-specs.md:112`) asserts that `callLLM returns an error of type *commit.PlannerBudgetExhaustedError`; this task establishes that type.

## Acceptance criteria

- Test asserts `errors.Is(&PlannerBudgetExhaustedError{}, ErrPlannerBudgetExhausted)` returns true.
- Test asserts `(&PlannerBudgetExhaustedError{Model: "X", Ceiling: 16384}).Error()` returns the sentinel string `"planner budget exhausted"`.
- Test asserts `errors.As(wrappedErr, &target)` where `wrappedErr = fmt.Errorf("plan commits: %w", &PlannerBudgetExhaustedError{Model: "M", Ceiling: 16384})` populates `target.Model == "M"` and `target.Ceiling == 16384`.

## Verification

```bash
go test -count=1 -run 'TestPlannerBudgetExhausted' ./domain/commit/...
```

Tests fail (RED) until task-002-impl lands.
