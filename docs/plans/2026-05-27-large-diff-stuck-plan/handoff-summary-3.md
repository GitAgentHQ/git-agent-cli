# Batch 3 Handoff Summary

**Batch:** 3 (token ceiling + DIFF-SYNOPSIS + heuristic planner)
**Verdict:** PASS (after recovery from coordinator stall)
**Tasks completed:** 6 (TaskList IDs #11, #12, #17, #18, #19, #20)
**Evaluator rework rounds:** 0 (passed on first verification after recovery)

## Recovery context

Original Batch 3 coordinator (agent ab94...) stalled mid-execution after ~13 min while updating P6 callLLM call sites — "no progress for 600s". Working tree was left with:
- P6 impl ~90% complete (constants + signature + ceiling branch + 2 of 4 call sites updated, **2 stranded call sites breaking the build**)
- P6 test (`TestClient_TokenCeiling`) actually already written by the stalled coordinator (discovered on recovery)
- P9 + P10 not started

Recovery coordinator (agent abb1...) picked up cleanly: fixed the two stranded call sites at `infrastructure/openai/client.go:551,581`, confirmed the pre-existing test passed against the existing impl, executed P9 and P10 fresh.

## Evidence

| Task | Verification command | Status |
|---|---|---|
| #11 (006-test, REQ-005) | `go test -count=1 -run 'TestClient_TokenCeiling' ./infrastructure/openai/...` | PASS |
| #12 (006-impl, REQ-005) | `go vet ./infrastructure/openai/...` + full pkg test | PASS |
| #17 (009-test, REQ-007) | `go test -count=1 -run 'TestCommitService_SynopsisFallback\|TestCommitService_TruncatorPathMultiFile' ./application/...` | PASS |
| #18 (009-impl, REQ-007) | `go test -count=1 ./application/...` | PASS |
| #19 (010-test, REQ-008) | `go test -count=1 -run 'TestDirectoryBucketer\|TestCommitService_HeuristicFallback' ./application/...` | PASS |
| #20 (010-impl, REQ-008) | `go test -count=1 ./application/... ./domain/commit/...` | PASS |
| batch-gate | `make test` (full suite) | PASS |
| marker-gate | `grep -r "TODO(batch-3)" . --include='*.go'` | PASS (no matches) |

## Modified files (8 + 2 new + 1 evaluation report)

- `infrastructure/openai/client.go` — `callLLM(maxTokens, maxTokensCeiling)`, four ceiling constants, ceiling-aware FinishReasonLength branch, four call sites updated
- `infrastructure/openai/client_test.go` — `TestClient_TokenCeiling`
- `application/commit_service.go` — saturation-check + `buildSynopsis` + `runPlan` fallback wrapper, `NewCommitService` signature gains `heuristicPlanner commit.HeuristicPlanner`
- `application/commit_service_test.go` — 14 `NewCommitService` call sites updated with `nil` for heuristicPlanner; `mockCommitGitClient` gains `stagedDiffStat` hook field + counter; `mockHookExecutor` gains `lastInput`/`inputs` capture; synopsis + heuristic-fallback tests
- `application/error_handling_test.go` — 1 `NewCommitService` call site updated with `nil`
- `application/heuristic_planner.go` (new) — `directoryBucketer` + `NewDirectoryBucketer()`
- `application/heuristic_planner_test.go` (new) — `TestDirectoryBucketer`
- `domain/commit/heuristic_planner.go` (new) — `HeuristicPlanner` interface (stdlib only)
- `cmd/commit.go` — `NewCommitService(..., nil)` for heuristicPlanner (Batch 4 will wire `NewDirectoryBucketer()` conditionally)

## Coordinator-flagged notes for downstream batches

1. **`NewCommitService` signature change rippled to 17 sites total** — 1 production (`cmd/commit.go`), 14 in `application/commit_service_test.go`, 1 in `application/error_handling_test.go`, plus 2 net-new test sites in `commit_service_test.go` (heuristic-fallback opt-in/opt-out). All non-Batch-3 sites pass `nil` for the new `heuristicPlanner` parameter.
2. **Pre-existing `TestCommitService_ByteCapAppliedBeforeGenerate` was updated** to use a 2-file group instead of 1-file. With Batch 3's saturation logic, a single-file saturated diff now triggers DIFF-SYNOPSIS instead of the raw byte-truncation path; the existing test was updated to preserve its original assertion (truncator-path semantics). The synopsis path has its own coverage in `TestCommitService_SynopsisFallbackOneFile`.
3. **Three `Plan` call sites in `commit_service.go` — only two get the fallback wrapper.** The hook-blocked re-plan site (~line 535) operates on a file list, not a saturating diff, so it intentionally stays on the bare `s.planner.Plan` call per `architecture.md` §2.2 (which only specifies fallback for the budget-exhausted path).
4. **`mockCommitGitClient.stagedDiffStat` is an optional hook field** — per-test override pattern; default remains `("", nil)`. Future tests exercising the synopsis path can set this field.

## Clean Architecture verification

- `domain/commit/heuristic_planner.go` imports only stdlib `context` ✓
- `application/heuristic_planner.go` imports stdlib (`path`, `strings`) + `domain/commit` + `domain/project` — no infrastructure ✓
- `infrastructure/openai/client.go` imports `domain/commit` (outer→inner) — allowed and unchanged from Batch 1's plumbing

## Recurring patterns

None. Batches 1, 2, 3 (after recovery) all passed evaluation on round 1 with zero rework rounds.

## Next batch

Batch 4 covers cmd-layer work that depends on Batch 1-3's foundation:
- pair 007 (cmd-signal) — tasks #13, #14 → modifies `main.go` + `cmd/root.go`; e2e test uses Batch 2's stall server
- pair 011 (cmd-budget-error) — tasks #21, #22 → switch arm for `*PlannerBudgetExhaustedError` in `cmd/commit.go`
- pair 012 (cmd-wire-config) — tasks #23, #24 → thread `request_timeout` / `heartbeat_interval` / `plan_fallback` into constructors at `cmd/commit.go`

P11 and P12 both modify `cmd/commit.go` — sequence them. P7 is independent.
