# Sprint Contract — Batch 5 (FINAL)

**Revision:** 1
**Batch composition:** 2 test-only tasks (sole remaining batch — ≥2 rule satisfied)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Tasks in this batch

| TaskList ID | Task file | Role |
|---|---|---|
| #25 | task-013-regression-test.md | test-only |
| #26 | task-014-security-test.md | test-only |

Both tasks add tests against already-shipped behaviour from Batches 1-4. No paired impl tasks. Independent of each other — can run in parallel within the batch.

## Goal

Lock in two invariants as guards against regression in future changes:

1. **Small-diff regression** — `git-agent commit` on a 1-file 200-byte fixture exits 0 with no heartbeat lines and ≤2 always-on phase lines. Confirms the new always-on output discipline doesn't bloat the small-diff path.
2. **No secret leakage** — heartbeat and phase lines emit only metadata. Sentinel API key and base URL strings never appear in captured stderr.

## Acceptance criteria (auto-derived from task BDD Then-clauses)

### Task #25 (regression, REQ-009)
- E2E test `TestCommitCmd_SmallDiffRegression` in `e2e/commit_test.go`:
  - Creates a temp git repo with 1 changed file containing a ~200-byte diff.
  - Reuses the existing fake-LLM server pattern (returns plan + per-group message quickly, <500ms each).
  - Runs `git-agent commit` as subprocess (no `--verbose`).
  - Asserts exit code 0.
  - Asserts captured stderr contains ZERO lines matching `"^still waiting on LLM..."`.
  - Asserts captured stderr contains AT MOST 2 lines matching the always-on phase patterns (e.g., `"planning"`, `"planned 1 commit(s)"` — single-file path skips the per-group "drafting" line because of the `len(allFiles) == 1` early-return at `application/commit_service.go:270-272`).
  - Asserts stdout contains the resulting commit hash and explanation.
- Cross-cutting: `make test` exits 0 with no test reporting goroutine leaks (already enforced by `goleak` in `infrastructure/openai`).
- Verification: `go test -count=1 -run 'TestCommitCmd_SmallDiffRegression' ./e2e/...` exits 0; `make test` exits 0.

### Task #26 (security, REQ-011)
- Unit test `TestClient_HeartbeatNoSecretLeakage` in `infrastructure/openai/client_test.go`:
  - Construct `openai.Client` with API key `"sk-secret-key-redact-me-001"`, base URL `"https://proxy.example.com/v1"`, model `"gpt-x"`, short `heartbeat_interval` (e.g., 50ms).
  - Slow-response server holds ≥150ms so ≥2 ticks fire.
  - Capture stderr buffer via `out io.Writer`.
  - Assert buffer contains ZERO occurrences of `"sk-secret-key-redact-me-001"` AND ZERO occurrences of `"proxy.example.com"`.
- Unit test `TestCommitService_PhaseLinesNoSecretLeakage` in `application/commit_service_test.go`:
  - Construct `CommitService` with fakes; `CommitRequest` whose `Intent` contains sentinel string `"SECRET-DIFF-CONTENT-NEVER-LOG"`; diff content also contains the sentinel.
  - Capture both `OutWriter` (always-on) and `LogWriter` (verbose) buffers.
  - Assert the always-on (`OutWriter`) buffer contains ZERO occurrences of the sentinel.
- Verification: `go test -count=1 -run 'TestClient_HeartbeatNoSecretLeakage|TestCommitService_PhaseLinesNoSecretLeakage' ./infrastructure/openai/... ./application/...` exits 0.

## Cross-cutting acceptance criteria

- After both tasks complete, `make test` exits 0.
- No prohibited Definition-of-Done patterns: no stubs, no `TODO`/`FIXME`, no `NotImplemented`, no empty function bodies.
- No code changes required to make these tests pass — both REQ-009 (regression) and REQ-011 (secret-leakage) are already satisfied by Batch 1-4 work. If a test FAILs, that signals a real regression and the coordinator must escalate via PIVOT rather than retro-fix Batch 1-4 work.
- The coordinator confirms with the main agent (via the structured return) which assertions actually fired and whether any required relaxation.

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if both tests pass on the existing (Batch 1-4) code and `make test` is green.
- **Rework budget:** maximum 2 evaluator-rework rounds. Note: since this batch adds tests only, rework should be limited to test-shape issues (assertion details, fake setup), NOT to changing production code.
- **Revision history:** initial revision.
