# Task 010 — Application heuristic planner fallback (test)

| Field | Value |
|---|---|
| **subject** | Add unit tests for `directoryBucketer` invocation + default-off behaviour |
| **type** | test |
| **depends-on** | ["002-domain-errors-impl"] |
| **REQ refs** | REQ-008 |
| **layer** | application |

## Files to modify

- `application/heuristic_planner_test.go` (new) — test `directoryBucketer.Plan`
- `application/commit_service_test.go` — add `TestCommitService_HeuristicFallback_OptIn`, `TestCommitService_HeuristicFallback_OptOut`

## BDD Scenarios

```gherkin
Scenario: Planner budget exhaustion triggers directory bucketing
  Given the LLM planner has returned commit.ErrPlannerBudgetExhausted
  When CommitService observes the exhausted error
  Then s.heuristicPlanner.Plan is invoked once with the same PlanRequest
  And directoryBucketer returns 3 commit groups: one per top-level directory
  And group 0 Files is ["cmd/<file>", "cmd/<file>"] mapped to scope "cli"
  And group 1 Files is ["application/<file>", "application/<file>"] mapped to scope "app"
  And group 2 Files is ["infrastructure/<file>", "infrastructure/<file>"] mapped to scope "infra"
  And stderr contains the line "planner exhausted budget — falling back to directoryBucketer"
  And the per-group Generate loop runs as normal
  And the process exits with exit code 0 on success
```

```gherkin
Scenario: With plan_fallback=none the existing hard-error path is preserved
  Given the project config at .git-agent/config.yml sets "plan_fallback: none"
  And the LLM planner has returned commit.ErrPlannerBudgetExhausted
  When CommitService observes the exhausted error
  Then s.heuristicPlanner.Plan is not invoked
  And the cmd layer renders the actionable budget-exhausted error to stderr
  And the process exits with exit code 1
```

## Acceptance criteria

- `directoryBucketer` unit test: feed `PlanRequest` with `UnstagedDiff.Files` = 6 paths across 3 top-level dirs; assert returned `CommitPlan.Groups` has 3 groups, each containing the 2 files from one dir, scope from `Config.Scopes` description match.
- Opt-in test: fake `CommitPlanner` returns `commit.ErrPlannerBudgetExhausted`; `req.Config.PlanFallback = "heuristic"`; spy `directoryBucketer` is invoked exactly once; service proceeds to per-group Generate loop.
- Opt-out test: same setup with `PlanFallback = "none"` (or absent); spy `directoryBucketer` is NOT invoked; service returns the budget-exhausted error.

## Verification

```bash
go test -count=1 -run 'TestDirectoryBucketer|TestCommitService_HeuristicFallback' ./application/...
```

Fails (RED) until task-010-impl lands.
