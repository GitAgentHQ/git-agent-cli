# Sprint Contract — Batch 4

**Revision:** 1
**Batch composition:** 3 Red-Green pairs (6 tasks total)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Tasks in this batch

| TaskList ID | Task file | Role |
|---|---|---|
| #13 | task-007-cmd-signal-test.md | RED |
| #14 | task-007-cmd-signal-impl.md | GREEN (depends on #13) |
| #21 | task-011-cmd-budget-error-render-test.md | RED |
| #22 | task-011-cmd-budget-error-render-impl.md | GREEN (depends on #21) |
| #23 | task-012-cmd-wire-config-test.md | RED |
| #24 | task-012-cmd-wire-config-impl.md | GREEN (depends on #23) |

**File overlap:** pairs P11 (#21/22) and P12 (#23/24) both modify `cmd/commit.go`. Execute P11 first, then P12, sequentially. Pair P7 (#13/14) modifies `main.go` + `cmd/root.go` + adds an e2e test in `e2e/commit_test.go` — independent of P11/P12.

## Goal

Wire the cmd layer to all the infrastructure work landed in Batches 1-3:

1. **SIGINT/SIGTERM cancellation** — `main.go` uses `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`, calls `cmd.ExecuteContext(ctx)`. `cmd/root.go` exposes `ExecuteContext(ctx)` alongside existing `Execute()`. The existing `defer` in `CommitService.Commit` already re-stages on error; cancellation routes through that path.
2. **`*PlannerBudgetExhaustedError` rendering** — `cmd/commit.go` error-path arm renders the actionable message with model + ceiling + concrete remediations; exits code 1.
3. **Config wiring** — `cmd/commit.go` threads `providerCfg.RequestTimeout`, `providerCfg.HeartbeatInterval`, `cmd.ErrOrStderr()` into `infraOpenAI.NewClient`. Reads `projCfg.PlanFallback`; constructs `application.NewDirectoryBucketer()` when `== project.PlanFallbackHeuristic`, else `nil`; passes to `application.NewCommitService`.

## Acceptance criteria (auto-derived from task BDD Then-clauses)

### Pair 7 (cmd SIGINT cancellation, REQ-002)
- `main.go` rewritten to use `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` with `defer stop()`, then call `cmd.ExecuteContext(ctx)`.
- `cmd/root.go` adds `ExecuteContext(ctx context.Context)` alongside existing `Execute()`. Shares exit-code mapping via a private helper.
- A new switch arm in the cmd error-path handling converts `context.Canceled` (from the application layer) to a clean stderr line `"cancelled"` and non-zero exit (exit code 1).
- E2E test (`TestCommitCmd_SIGINTCancels`):
  - Adds (or reuses) a stall-server helper to `e2e/helpers_test.go` (or new `e2e/fakellm_test.go`) — the openai-package helper is internal to that package; e2e needs its own.
  - Creates a temp repo with three files; pre-stages `a.go` and `b.go`.
  - Launches `git-agent commit` as a subprocess; waits ~200ms for the call to start; sends `cmd.Process.Signal(os.Interrupt)`.
  - Asserts process exits within 1 second of the signal with non-zero exit code.
  - Asserts `git diff --staged --name-only` lists exactly `a.go` and `b.go` (original pre-stage state preserved by the existing recovery defer).
  - Asserts stderr contains `"cancelled"` and does NOT contain `"panic"` or `"goroutine"`.
- Verification: `go test -count=1 -run 'TestCommitCmd_SIGINTCancels' ./e2e/...` exits 0.

### Pair 11 (cmd budget-error rendering, REQ-006)
- `cmd/commit.go` adds a new switch arm in `runCommit`'s error-path (after the existing `*agentErrors.APIError` arm, ~line 173): `var budgetErr *commit.PlannerBudgetExhaustedError; if errors.As(err, &budgetErr) { /* render + return exit 1 */ }`.
- Render template (verbatim): `"error: LLM kept producing oversized output (model=%s, ceiling=%d tokens); try a more capable model, narrow scope with --intent, or split with --max-diff-lines / smaller batches\n"` to `cmd.ErrOrStderr()`.
- Return `agentErrors.NewExitCodeError(1, "")` (empty message — avoids double-printing).
- Test (`TestCommit_RenderBudgetExhausted`): invoke `runCommit` (or a refactored helper) where the application returns `fmt.Errorf("plan commits: %w", &commit.PlannerBudgetExhaustedError{Model: "deepseek-v4-flash", Ceiling: 16384})`. Capture stderr. Assert contains `"model=deepseek-v4-flash"`, `"ceiling=16384"`, at least 2 of the 5 remediation phrases (`--max-diff-lines`, `--max-diff-bytes`, `--intent`, `try a more capable model`, `commit smaller batches`). Assert returned `*ExitCodeError` carries `Code == 1`. Assert stderr does NOT contain `"max_tokens=32768"` (no leftover from old doubling path).
- Verification: `go test -count=1 -run 'TestCommit_RenderBudgetExhausted' ./cmd/...` exits 0.

### Pair 12 (cmd config wiring)
- `cmd/commit.go` after `providerCfg, err := resolveProviderConfig(cmd)`:
  - Reads `providerCfg.RequestTimeout` and `providerCfg.HeartbeatInterval`.
  - Passes both into the `infraOpenAI.NewClient(...)` call at the existing site, plus `cmd.ErrOrStderr()` as the `out io.Writer` so heartbeat lines surface to stderr.
  - Reads `projCfg.PlanFallback`. When `== project.PlanFallbackHeuristic`, constructs `heuristicPlanner := application.NewDirectoryBucketer()`; else `nil`.
  - Passes `heuristicPlanner` into `application.NewCommitService(...)` as the new last argument.
- Existing `--free` path continues to work (provider config resolution unchanged).
- Test (`TestCommit_WiresConfigToConstructors`):
  - Loads a user-config YAML fixture with `request_timeout: 5s`, `heartbeat_interval: 2s`.
  - Loads a project-config YAML fixture with `plan_fallback: heuristic`.
  - Refactors `runCommit` into a helper if needed for testability (the alternative is a spy `NewClient` / `NewCommitService` via test-only constructors; choose the simpler option).
  - Asserts the resulting `openai.Client` reports `requestTimeout == 5*time.Second`, `heartbeatInterval == 2*time.Second`.
  - Asserts `CommitService` is constructed with a non-nil `heuristicPlanner` when `PlanFallback == "heuristic"`, nil when `PlanFallback == "none"`.
- Verification: `go test -count=1 -run 'TestCommit_WiresConfigToConstructors' ./cmd/...` exits 0.

## Cross-cutting acceptance criteria

- After all six tasks complete, `make test` exits 0.
- No prohibited Definition-of-Done patterns: no stubs, no `TODO`/`FIXME`, no `NotImplemented`, no empty function bodies.
- Each new test was observed to FAIL before its paired implementation landed.
- After Batch 4, the runtime state with `request_timeout: 5s` in user config and `plan_fallback: heuristic` in project config produces an `openai.Client` with 5s timeout and a `CommitService` with non-nil `heuristicPlanner` — this is the integration outcome the user actually experiences.
- The `defer` re-stage in `CommitService.Commit` (lines 347-361) continues to work under SIGINT — verify via the e2e test that the working tree returns to its pre-invocation staged state.

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if every acceptance criterion above is satisfied and the cross-cutting checks pass.
- **Rework budget:** maximum 2 evaluator-rework rounds before escalation.
- **Revision history:** initial revision.
