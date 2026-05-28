# Task 002 — Domain errors (impl)

| Field | Value |
|---|---|
| **subject** | Add `commit.ErrPlannerBudgetExhausted` sentinel and `PlannerBudgetExhaustedError` carrier |
| **type** | impl |
| **depends-on** | ["002-domain-errors-test"] |
| **REQ refs** | REQ-005, REQ-006 |
| **layer** | domain/commit |

## Files to create

- `domain/commit/errors.go`

## Interface contracts

```go
package commit

import "errors"

// ErrPlannerBudgetExhausted is the sentinel returned (wrapped) by the
// infrastructure LLM client when token doubling would exceed the
// per-endpoint ceiling.
var ErrPlannerBudgetExhausted = errors.New("planner budget exhausted")

// PlannerBudgetExhaustedError carries the model name and ceiling so the
// cmd layer can render an actionable message.
type PlannerBudgetExhaustedError struct {
    Model   string
    Ceiling int
}

func (e *PlannerBudgetExhaustedError) Error() string { /* ... */ }
func (e *PlannerBudgetExhaustedError) Is(target error) bool { /* ... */ }
```

## Implementation steps

1. Create `domain/commit/errors.go` with the sentinel + carrier types.
2. Implement `Error()` to return the sentinel string.
3. Implement `Is(target)` to match `ErrPlannerBudgetExhausted`.
4. No other domain or external imports beyond stdlib `errors`.

## Verification

```bash
go test -count=1 ./domain/commit/...
```

Task-002-test cases turn green. Domain package retains zero external imports.
