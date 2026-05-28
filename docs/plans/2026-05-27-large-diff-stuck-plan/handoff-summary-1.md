# Batch 1 Handoff Summary

**Batch:** 1 (foundation)
**Verdict:** PASS
**Tasks completed:** 6 (TaskList IDs #1-6)
**Evaluator rework rounds:** 0 (passed on first verification)

## Evidence

| Task | Verification command | Status |
|---|---|---|
| #1 (001-test) | `go test -count=1 -run 'TestKeys\|TestResolve\|TestProjectConfig' ./infrastructure/config/...` | PASS |
| #2 (001-impl) | `go test -count=1 ./infrastructure/config/...` | PASS |
| #3 (002-test) | `go test -count=1 -run 'TestPlannerBudgetExhausted' ./domain/commit/...` | PASS |
| #4 (002-impl) | `go test -count=1 ./domain/commit/...` | PASS |
| #5 (003-test) | `go test -count=1 -run 'TestClient_StagedDiffStat' ./infrastructure/git/...` | PASS |
| #6 (003-impl) | `go test -count=1 ./infrastructure/git/... ./application/...` | PASS |
| batch-gate | `make test` (full suite) | PASS |

All RED phases observed to fail before their GREEN counterparts (compile errors on undefined types/methods).

## Modified files (12)

- `application/commit_service.go` — `CommitGitClient` interface extended with `StagedDiffStat(ctx) (string, error)`
- `application/commit_service_test.go` — shared `mockCommitGitClient` fake extended with `StagedDiffStat` stub
- `domain/commit/errors.go` (new) — `ErrPlannerBudgetExhausted` sentinel + `PlannerBudgetExhaustedError` carrier
- `domain/commit/errors_test.go` (new) — round-trip tests for the typed error
- `domain/project/config.go` — `PlanFallback` field added (note: file path was domain/project, not infrastructure/config as plan said — see correction note below)
- `infrastructure/config/keys.go` — `request_timeout` / `heartbeat_interval` / `plan_fallback` registry + kebab aliases
- `infrastructure/config/project.go` — project-config layer of `plan_fallback` plumbing
- `infrastructure/config/project_test.go` — round-trip tests for `plan_fallback`
- `infrastructure/config/resolver.go` — `ProviderConfig.{RequestTimeout, HeartbeatInterval}` + defaults
- `infrastructure/config/resolver_test.go` — Resolve tests for the two duration keys
- `infrastructure/git/client.go` — `StagedDiffStat(ctx)` method
- `infrastructure/git/client_test.go` — `TestClient_StagedDiffStat`

## Notes for downstream batches

- Only **one** `CommitGitClient` fake exists in the repo: `mockCommitGitClient` in `application/commit_service_test.go`, shared via package scope across `commit_service_test.go`, `error_handling_test.go`, `verbose_test.go`. The plan's mention of `add_service_test.go` was incorrect — that file's `mockGitClient` implements a different interface (`AddGitClient`).
- Auxiliary registry plumbing added: `coerceForWrite` preserves canonical duration strings on write; `NormalizeValue` validates duration input via `time.ParseDuration`. Without this, `git-agent config set request-timeout 45s` would have stored the raw string with no validation. This addresses a latent gap not explicitly called out in the plan.
- `project.PlanFallbackNone = "none"` and `project.PlanFallbackHeuristic = "heuristic"` constants are available for downstream batches (Batch 3 will need them in `application/heuristic_planner.go`).
- The evaluator ran inline within the coordinator (subagent-spawn was not available in that context). Report written to `evaluation-round-1-batch-1.md`.

## Recurring patterns

None. First batch.

## Next batch

Batch 2 covers Tier-1 pairs that depend on Batch 1's foundation:
- pair 004 (openai-http-timeout) — tasks #7, #8
- pair 005 (openai-heartbeat) — tasks #9, #10
- pair 008 (app-phase-output) — tasks #15, #16 (no Batch-1 dep, but logically next; pair P8 in plan numbering is Tier 0 — included here to keep batch size at 6)
