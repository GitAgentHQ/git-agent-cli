# Sprint Contract — Batch 3

**Revision:** 1
**Batch composition:** 3 Red-Green pairs (6 tasks total)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Tasks in this batch

| TaskList ID | Task file | Role |
|---|---|---|
| #11 | task-006-openai-token-ceiling-test.md | RED |
| #12 | task-006-openai-token-ceiling-impl.md | GREEN (depends on #11) |
| #17 | task-009-app-synopsis-fallback-test.md | RED |
| #18 | task-009-app-synopsis-fallback-impl.md | GREEN (depends on #17) |
| #19 | task-010-app-heuristic-fallback-test.md | RED |
| #20 | task-010-app-heuristic-fallback-impl.md | GREEN (depends on #19) |

**File overlap:** pairs P9 (#17/18) and P10 (#19/20) both modify `application/commit_service.go`. Execute P9 first, then P10, sequentially. Pair P6 (#11/12) modifies `infrastructure/openai/client.go` independently.

## Goal

Three independent remediations that together close out REQ-005, REQ-007, and REQ-008:

1. **Token-doubling ceiling** — replace the unbounded `req.MaxCompletionTokens *= 2` block in `callLLM` (pinned by the `TODO(batch-3)` marker at `infrastructure/openai/client.go:304`) with a ceiling-aware branch that returns `*commit.PlannerBudgetExhaustedError` when the next doubling would exceed the per-endpoint ceiling. Extend `callLLM` signature with `maxTokensCeiling int`; pass per-endpoint constants from the four call sites.
2. **DIFF-SYNOPSIS fallback** — in `CommitService.Commit`, after `s.truncator.Truncate`, check for single-file saturation; on hit, call `s.git.StagedDiffStat(ctx)` and substitute a `DIFF-SYNOPSIS` block built by `buildSynopsis` for the LLM payload. Hook still receives the raw diff.
3. **Heuristic planner fallback** — add `domain/commit/heuristic_planner.go` (interface only), `application/heuristic_planner.go` (`directoryBucketer` impl + `NewDirectoryBucketer()` constructor), and wire the fallback into the two `Plan` call sites in `CommitService.Commit` behind the `plan_fallback: heuristic` opt-in. `NewCommitService` signature gains `heuristicPlanner commit.HeuristicPlanner` as the new last parameter.

## Acceptance criteria (auto-derived from task BDD Then-clauses)

### Pair 6 (openai token ceiling, REQ-005)
- Four package-scope constants declared in `infrastructure/openai/client.go`: `planMaxTokensCeiling=16384`, `generateMaxTokensCeiling=16384`, `scopesMaxTokensCeiling=16384`, `detectMaxTokensCeiling=4096`.
- `callLLM` signature extended to `(ctx, system, user string, maxTokens, maxTokensCeiling int) (string, error)`.
- Four call sites updated to pass the matching ceiling: `Generate` → `generateMaxTokensCeiling`, `Plan` → `planMaxTokensCeiling`, `DetectTechnologies` → `detectMaxTokensCeiling`, `GenerateScopes` → `scopesMaxTokensCeiling`.
- The `TODO(batch-3)` marker at `infrastructure/openai/client.go:304` is removed; the unbounded `req.MaxCompletionTokens *= 2` block is replaced with: compute `next := req.MaxCompletionTokens * 2`; if `next > maxTokensCeiling`, return `&commit.PlannerBudgetExhaustedError{Model: c.model, Ceiling: maxTokensCeiling}` immediately; else assign and `continue` the retry loop.
- Test (`TestClient_TokenCeiling`): `httptest.NewServer` returns a canned `finish_reason=length` response per call; count incoming requests. Invoke `client.Plan(ctx, req)` (or equivalent direct call) with seed `MaxCompletionTokens=8192` and ceiling `16384`. Assert exactly 2 HTTP requests reach the server (not 3). Assert `errors.As(err, &target)` populates `*commit.PlannerBudgetExhaustedError` with `Model == "deepseek-v4-flash"` and `Ceiling == 16384`. Assert `errors.Is(err, commit.ErrPlannerBudgetExhausted)` returns true.
- Verification: `go test -count=1 -run 'TestClient_TokenCeiling' ./infrastructure/openai/...` exits 0.

### Pair 9 (application DIFF-SYNOPSIS fallback, REQ-007)
- After the `s.truncator.Truncate` call in `CommitService.Commit` (~line 397 region), insert: `if didTruncate && len(genDiff.Files) == 1 && len(genDiff.Content) == maxBytes` → call `s.git.StagedDiffStat(ctx)`, parse the single-file `+<num> -<num>` counts from the stat line, build a `DIFF-SYNOPSIS` block via a new private `buildSynopsis(file, statLine string, actualBytes, capBytes int) string`, and replace `genDiff.Content` with the synopsis string. Do NOT touch `groupDiff.Content` (hook still sees raw diff).
- On `StagedDiffStat` error, `s.vlog` the error and continue with the truncated path (do not fail the commit).
- Always-on lines: `commit %d/%d: DIFF-SYNOPSIS fallback (%s)` when fallback triggers; `commit %d/%d: truncating group diff (%d bytes)` when normal truncator path runs.
- `DIFF-SYNOPSIS` block format (verbatim per `architecture.md` §4.1):
  ```
  DIFF-SYNOPSIS
  file: <path>
  changes: +<adds> / -<dels> (stat)
  note: full diff elided (<actualBytes> bytes exceeded <cap>-byte cap)
  ```
  When the stat parse fails, default to `+0 / -0`.
- Test (`TestCommitService_SynopsisFallbackOneFile`): fake `CommitGitClient` with `StagedDiff` returning 1 file with 1 MB content; `StagedDiffStat` returning the canned stat string; spy `Generate` captures the `Diff.Content` sent to the LLM. Assert the captured content starts with `"DIFF-SYNOPSIS"`, contains the three labelled lines, total length under 4096. Assert `HookInput.Diff` is the original 1 MB.
- Test (`TestCommitService_TruncatorPathMultiFile`): 4-file group with 500000 bytes total; assert captured `Diff.Content` does NOT start with `"DIFF-SYNOPSIS"`; assert `StagedDiffStat` is never invoked (count via spy).
- Verification: `go test -count=1 -run 'TestCommitService_SynopsisFallback|TestCommitService_TruncatorPathMultiFile' ./application/...` exits 0.

### Pair 10 (application heuristic fallback, REQ-008)
- `domain/commit/heuristic_planner.go` (new) declares `HeuristicPlanner` interface with `Plan(ctx context.Context, req PlanRequest) (*CommitPlan, error)`. Imports only stdlib `context`.
- `application/heuristic_planner.go` (new) declares unexported `directoryBucketer` struct + `NewDirectoryBucketer() commit.HeuristicPlanner` constructor.
- `directoryBucketer.Plan` bucketing rule: group files by `strings.SplitN(file, "/", 2)[0]` (first path component). When `req.Config.Scopes` non-empty, map each top-level dir to the scope whose description contains the dir name (case-insensitive substring); unmapped dirs use empty scope. Cap at `maxCommitGroups` (5) — merge smallest buckets into the last group. Title placeholder: `"chore(<scope>): update N files in <dir>/"` (scoped) or `"chore: update N files in <dir>/"` (unscoped).
- `CommitService` struct gains `heuristicPlanner commit.HeuristicPlanner` field.
- `NewCommitService` signature gains `heuristicPlanner commit.HeuristicPlanner` as new last parameter.
- Both `Plan` call sites in `CommitService.Commit` (~lines 275-283 and ~312-320 region) wrapped in fallback pattern: on `errors.Is(err, commit.ErrPlannerBudgetExhausted)` AND `s.heuristicPlanner != nil` AND `req.Config != nil` AND `req.Config.PlanFallback == project.PlanFallbackHeuristic`, retry via `s.heuristicPlanner.Plan(ctx, planReq)`.
- Always-on line: `"planner exhausted budget — falling back to directoryBucketer"` emitted via `s.out(req, ...)` on fallback path.
- Existing `NewCommitService` call sites (`cmd/commit.go` etc.) updated to pass `nil` for the new param; Batch 4's task #24 will wire `NewDirectoryBucketer()` when `PlanFallback == "heuristic"`.
- Test (`TestDirectoryBucketer`): feed `PlanRequest` with 6 paths across 3 top-level dirs; assert returned `CommitPlan.Groups` has 3 groups, each containing the 2 files from one dir, scope from `Config.Scopes` description match.
- Test (`TestCommitService_HeuristicFallback_OptIn`): fake `CommitPlanner` returns `commit.ErrPlannerBudgetExhausted`; `req.Config.PlanFallback = "heuristic"`; spy `directoryBucketer` invoked exactly once; service proceeds to per-group `Generate` loop.
- Test (`TestCommitService_HeuristicFallback_OptOut`): same setup with `PlanFallback = "none"` (or absent); spy `directoryBucketer` NOT invoked; service returns the budget-exhausted error.
- Verification: `go test -count=1 -run 'TestDirectoryBucketer|TestCommitService_HeuristicFallback' ./application/...` exits 0.

## Cross-cutting acceptance criteria

- After all six tasks complete, `make test` exits 0.
- `domain/commit/heuristic_planner.go` imports only stdlib (`context`).
- The `TODO(batch-3)` marker is gone from the codebase (`grep -r "TODO(batch-3)" .` returns zero matches).
- `NewCommitService` signature change rippled correctly — every call site updated.
- The shared `mockCommitGitClient` fake in `application/commit_service_test.go` may need its `StagedDiffStat` stub overridden in synopsis-fallback tests (use a per-test override pattern).
- No prohibited Definition-of-Done patterns.
- Each new test was observed to FAIL before its paired implementation landed.

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if every acceptance criterion above is satisfied and the cross-cutting checks pass.
- **Rework budget:** maximum 2 evaluator-rework rounds before escalation.
- **Revision history:** initial revision.
