# Sprint Contract — Batch 1

**Revision:** 1
**Batch composition:** 3 Red-Green pairs (6 tasks total)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Tasks in this batch

| TaskList ID | Task file | Role |
|---|---|---|
| #1 | task-001-config-keys-test.md | RED |
| #2 | task-001-config-keys-impl.md | GREEN (depends on #1) |
| #3 | task-002-domain-errors-test.md | RED |
| #4 | task-002-domain-errors-impl.md | GREEN (depends on #3) |
| #5 | task-003-git-stagediffstat-test.md | RED |
| #6 | task-003-git-stagediffstat-impl.md | GREEN (depends on #5) |

These three pairs are independent of each other — they can be executed in parallel within the batch. Each pair must follow RED→GREEN ordering: write the failing test first, watch it fail, then add the implementation that makes it pass.

## Goal

Establish the foundation surface that downstream batches depend on:

1. Three new config keys (`request_timeout`, `heartbeat_interval`, `plan_fallback`) registered in the resolver + project config (`ProviderConfig.RequestTimeout`, `ProviderConfig.HeartbeatInterval`, `project.Config.PlanFallback`).
2. Two new typed errors in `domain/commit/`: sentinel `ErrPlannerBudgetExhausted` + carrier `PlannerBudgetExhaustedError{Model, Ceiling}`.
3. One new git method `(*git.Client).StagedDiffStat(ctx) (string, error)` plus its signature on the `CommitGitClient` application interface, with stub additions to every existing fake in `application/*_test.go`.

## Acceptance criteria (auto-derived from task BDD Then-clauses)

### Pair 1 (config keys, REQ-001 / REQ-004 / REQ-008)
- `infrastructure/config/keys.go` registers `request_timeout` (Type `duration`, user-scope only) with kebab alias `request-timeout`.
- `infrastructure/config/keys.go` registers `heartbeat_interval` (Type `duration`, user-scope only) with kebab alias `heartbeat-interval`.
- `infrastructure/config/keys.go` registers `plan_fallback` (Type `string`, project + local scope) with kebab alias `plan-fallback`.
- `infrastructure/config/resolver.go` declares `DefaultRequestTimeout = 90 * time.Second` and `DefaultHeartbeatInterval = 15 * time.Second`.
- `infraConfig.Resolve(...)` populates `ProviderConfig.RequestTimeout` and `ProviderConfig.HeartbeatInterval` from a YAML fixture, falling back to defaults when absent.
- `project.Config.PlanFallback` round-trips through YAML marshal / unmarshal with values `"none"` and `"heuristic"`; absent value yields the zero string.
- `project.PlanFallbackNone = "none"` and `project.PlanFallbackHeuristic = "heuristic"` constants exist.
- Verification: `go test -count=1 ./infrastructure/config/...` exits 0.

### Pair 2 (domain errors, REQ-005 / REQ-006)
- `domain/commit/errors.go` exists; imports only stdlib (`errors`).
- `errors.Is(&PlannerBudgetExhaustedError{}, ErrPlannerBudgetExhausted)` returns true.
- `(&PlannerBudgetExhaustedError{Model: "X", Ceiling: 16384}).Error()` returns the sentinel string `"planner budget exhausted"`.
- `errors.As(fmt.Errorf("plan commits: %w", &PlannerBudgetExhaustedError{Model: "M", Ceiling: 16384}), &target)` populates `target.Model == "M"` and `target.Ceiling == 16384`.
- Verification: `go test -count=1 ./domain/commit/...` exits 0.

### Pair 3 (git StagedDiffStat, REQ-007 prerequisite)
- `(*git.Client).StagedDiffStat(ctx) (string, error)` exists in `infrastructure/git/client.go`; runs `git diff --staged --stat --ignore-submodules=all`.
- `CommitGitClient` interface in `application/commit_service.go` includes the new method signature.
- Every fake implementing `CommitGitClient` in `application/*_test.go` adds a `StagedDiffStat(ctx) (string, error)` stub returning `("", nil)` (or canned strings for tests that exercise REQ-007 in later batches).
- The unit test creates a temp git repo with one staged 1 MB file and asserts the returned string contains the filename and a `+<num>` insertion count and ends with a `1 file changed, ` summary.
- Verification: `go test -count=1 ./infrastructure/git/... ./application/...` exits 0.

## Cross-cutting acceptance criteria

- After all six tasks complete, `make test` (or `go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`) exits 0.
- `domain/commit/` retains zero external imports (stdlib only).
- No prohibited patterns: no stubs, no `TODO`/`FIXME`, no `NotImplemented`, no empty function bodies returning hardcoded defaults.
- Each new test was observed to FAIL before its paired implementation landed (RED state confirmed) and to PASS after (GREEN state confirmed).

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if every acceptance criterion above is satisfied and the cross-cutting checks pass.
- **Rework budget:** maximum 2 evaluator-rework rounds before escalation per `references/blocker-and-escalation.md`.
- **Revision history:** initial revision.
