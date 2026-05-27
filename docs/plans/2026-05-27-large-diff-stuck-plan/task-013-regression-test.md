# Task 013 — Regression (test-only)

| Field | Value |
|---|---|
| **subject** | Confirm small-diff happy path is unchanged and full test suite passes after the redesign |
| **type** | test |
| **depends-on** | ["004-openai-http-timeout-impl", "005-openai-heartbeat-impl", "006-openai-token-ceiling-impl", "007-cmd-signal-impl", "008-app-phase-output-impl", "009-app-synopsis-fallback-impl", "010-app-heuristic-fallback-impl", "011-cmd-budget-error-render-impl", "012-cmd-wire-config-impl"] |
| **REQ refs** | REQ-009 |
| **layer** | e2e |

## Files to modify

- `e2e/commit_test.go` — add `TestCommitCmd_SmallDiffRegression`

## BDD Scenarios

```gherkin
Scenario: Existing small-diff happy path is unchanged
  Given a git repository at /tmp/tiny-diff-fixture with 1 changed file containing a 200-byte diff
  And a fake LLM endpoint that returns a plan and a single message in under 1 second each
  When I run "git-agent commit" without --verbose
  Then the process exits with exit code 0
  And stderr contains zero heartbeat lines matching "^still waiting on LLM..."
  And stderr contains at most 2 always-on phase lines
  And stdout contains the resulting commit hash and explanation
```

```gherkin
Scenario: go test ./... passes after the redesign
  Given the working tree contains the redesign changes
  When I run "go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/..."
  Then every test package reports PASS
  And no test reports a goroutine leak
```

## Acceptance criteria

- Small-diff e2e test creates a 1-file 200-byte fixture; runs the subprocess against a fake LLM that responds in <500ms; asserts exit 0; greps stderr for `"still waiting on LLM"` (must be absent).
- Full suite passes: a Make target `make test` (existing) exits 0.

## Verification

```bash
go test -count=1 -run 'TestCommitCmd_SmallDiffRegression' ./e2e/...
make test
```

No implementation task. This is a guard test that locks in the absence of regression.
