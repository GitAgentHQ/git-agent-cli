# Task 011 — Cmd budget-error rendering (impl)

| Field | Value |
|---|---|
| **subject** | Add switch arm for `*PlannerBudgetExhaustedError`; render actionable message |
| **type** | impl |
| **depends-on** | ["011-cmd-budget-error-render-test"] |
| **REQ refs** | REQ-006 |
| **layer** | cmd |

## Files to modify

- `cmd/commit.go` — extend `errors.As` chain in the error-path arm

## Implementation steps

1. After the existing `*agentErrors.APIError` arm in `runCommit` (~line 173), add a new arm: `var budgetErr *commit.PlannerBudgetExhaustedError; if errors.As(err, &budgetErr) { /* render */ }`.
2. Render template: `"error: LLM kept producing oversized output (model=%s, ceiling=%d tokens); try a more capable model, narrow scope with --intent, or split with --max-diff-lines / smaller batches\n"` written to `cmd.ErrOrStderr()`.
3. Return `agentErrors.NewExitCodeError(1, "")` (with empty message to avoid double-printing — the human-readable line was already written via `Fprintf`).
4. Remove the existing `"LLM exhausted token limit (model=X, max_tokens=Y, attempt=Z/N)"` text from `infrastructure/openai/client.go`'s `FinishReasonLength` branch (task-006 already replaced it with the typed error return; verify nothing else generates that legacy string).

## Verification

```bash
go test -count=1 ./cmd/...
```

Task-011-test passes.
