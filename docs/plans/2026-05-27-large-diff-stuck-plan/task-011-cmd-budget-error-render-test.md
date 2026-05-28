# Task 011 — Cmd budget-error rendering (test)

| Field | Value |
|---|---|
| **subject** | Add unit test for cmd-layer rendering of `*PlannerBudgetExhaustedError` |
| **type** | test |
| **depends-on** | ["002-domain-errors-impl", "006-openai-token-ceiling-impl"] |
| **REQ refs** | REQ-006 |
| **layer** | cmd |

## Files to modify

- `cmd/commit_test.go` — add `TestCommit_RenderBudgetExhausted`

## BDD Scenario

```gherkin
Scenario: Budget-exhausted error names the model and at least two remediations
  Given the planner has returned commit.ErrPlannerBudgetExhausted
  When the cmd layer renders the error to stderr
  Then stderr contains the literal substring "model=deepseek-v4-flash"
  And stderr contains the literal substring "ceiling=16384"
  And stderr contains at least two of the following remediation strings: "--max-diff-lines", "--max-diff-bytes", "--intent", "try a more capable model", "commit smaller batches"
  And the process exits with exit code 1
  And stderr does not contain the substring "max_tokens=32768" (no leftover from the old doubling path)
```

## Acceptance criteria

- Test invokes `runCommit` (or a refactored helper) where the application returns `fmt.Errorf("plan commits: %w", &commit.PlannerBudgetExhaustedError{Model: "deepseek-v4-flash", Ceiling: 16384})`.
- Captures stderr; asserts contains `"model=deepseek-v4-flash"` and `"ceiling=16384"`.
- Asserts contains at least 2 of the 5 remediation phrases (verify via count).
- Asserts returned `*ExitCodeError` carries `Code == 1`.
- Asserts stderr does NOT contain `"max_tokens=32768"` (old doubling text).

## Verification

```bash
go test -count=1 -run 'TestCommit_RenderBudgetExhausted' ./cmd/...
```

Fails (RED) until task-011-impl lands.
