# Sprint Contract — Batch 2

**Revision:** 1
**Batch composition:** 3 Red-Green pairs (6 tasks total)
**Plan:** `docs/plans/2026-05-27-large-diff-stuck-plan/`
**Code checklist:** `docs/retros/checklists/code-v1.md`

## Tasks in this batch

| TaskList ID | Task file | Role |
|---|---|---|
| #7 | task-004-openai-http-timeout-test.md | RED |
| #8 | task-004-openai-http-timeout-impl.md | GREEN (depends on #7) |
| #9 | task-005-openai-heartbeat-test.md | RED |
| #10 | task-005-openai-heartbeat-impl.md | GREEN (depends on #9) |
| #15 | task-008-app-phase-output-test.md | RED |
| #16 | task-008-app-phase-output-impl.md | GREEN (depends on #15) |

Pairs 4 (http-timeout) and 5 (heartbeat) both modify `infrastructure/openai/client.go` — they share files and MUST be executed sequentially with care to avoid stomping on each other's edits. Pair 8 (app-phase-output) modifies `application/commit_service.go`, independent of pairs 4 and 5.

## Goal

Bound LLM HTTP calls in time and visibility:

1. Per-attempt HTTP timeout via `cfg.HTTPClient = &http.Client{Timeout: requestTimeout}` + `context.WithTimeout(ctx, c.requestTimeout)` per attempt. Timeouts trigger retries instead of hanging.
2. 15-second heartbeat goroutine emitting "still waiting on LLM..." to a configured stderr writer during in-flight `CreateChatCompletion` calls.
3. Default-mode phase output: 8 existing `s.vlog` call sites in `application/commit_service.go` promoted to `s.out` (per the table in `architecture.md` §2.2), plus the per-group LLM-call line rephrased to include group context (`commit i/N: drafting message (attempt j/k)`).

## Acceptance criteria (auto-derived from task BDD Then-clauses)

### Pair 4 (openai HTTP timeout, REQ-001)
- `NewClient` signature extended to `(apiKey, baseURL, model string, requestTimeout, heartbeatInterval time.Duration, out io.Writer)`.
- When `requestTimeout <= 0`, the constructor uses `90 * time.Second` default.
- `cfg.HTTPClient = &http.Client{Timeout: requestTimeout}` is set in `NewClient`.
- `callLLM` wraps each `CreateChatCompletion` call with `context.WithTimeout(ctx, c.requestTimeout); defer cancel()`.
- On `errors.Is(err, context.DeadlineExceeded)`, `lastErr` is set to `"request timed out after %s (model=%s, attempt=%d/%d)"` and the retry loop continues.
- On `errors.Is(err, context.Canceled)`, the error is returned immediately (no retry — propagates SIGINT cleanly).
- Test (`TestClient_PerAttemptTimeout`): a `httptest.NewServer` with hijack-and-never-write handler; `NewClient(..., 1*time.Second, 0, &buf)`; `Generate` returns within ~3.5 s; error contains `"request timed out after 1s"` and `"model=<configured>"`; does NOT contain `"context.DeadlineExceeded"` raw or `"panic"`.
- Verification: `go test -count=1 -run 'TestClient_PerAttemptTimeout' ./infrastructure/openai/...` exits 0.

### Pair 5 (openai heartbeat, REQ-004)
- `Client` struct gains `heartbeatInterval time.Duration` field.
- `(*Client).heartbeat(ctx context.Context, done <-chan struct{})` private method exists.
- For each in-flight `CreateChatCompletion` attempt, `callLLM` spawns `go c.heartbeat(attemptCtx, done)`; closes `done` after the call returns; `cancel()`s the attempt context after the goroutine cleans up.
- The heartbeat exits within 100 ms of either `done` close or `ctx.Done()`.
- Line format: `"still waiting on LLM... (%ds elapsed, model=%s)\n"` written via `fmt.Fprintf(c.out, ...)`.
- When `c.out == nil`, the heartbeat returns immediately (no-op).
- `go.uber.org/goleak` added via `go get`; `goleak.VerifyTestMain(m)` in `infrastructure/openai/client_test.go`.
- Test (`TestClient_HeartbeatTicks`): for speed use `heartbeat_interval=100ms`, slow-response `sleep=350ms` so ~3 ticks fire in <1s; capture stderr; assert exact tick count and `model=<configured>` substring.
- Verification: `go test -count=1 -run 'TestClient_HeartbeatTicks' ./infrastructure/openai/...` exits 0; no goleak failure.

### Pair 8 (application phase output, REQ-003 + REQ-010)
- 9 existing `s.vlog` sites in `application/commit_service.go` swapped to `s.out` per the table in `architecture.md` §2.2 (lines 242, 245, 274, 291, 298, 311, 338, 416, 561 with the original message text preserved at all sites except line 416).
- Line 416 rephrased from `"calling LLM... (attempt %d/%d)"` to `"commit %d/%d: drafting message (attempt %d/%d)"`; the loop index `i` (0-based becomes 1-based for output) and `len(plan.Groups)` are threaded into scope.
- No promoted site keeps a sibling `s.vlog` call (verify no duplication on `--verbose`).
- Test (`TestCommitService_AlwaysOnPhaseLines`): drive `Commit` with `Verbose: false` and a `bytes.Buffer` `OutWriter`; assert stderr contains `"planning"`, `"planned 2 commit(s)"`, `"commit 1/2: drafting message (attempt 1/3)"`, `"commit 2/2: drafting message (attempt 1/3)"`; assert no per-file diff content.
- Test (`TestCommitService_VerboseIsSuperset`): two runs (verbose on/off) against the same fakes; every line in the non-verbose stream appears exactly once in the verbose stream; verbose stream has additional lines including `"staged files:"` and `"unstaged files:"`; no line duplicated within either stream.
- Verification: `go test -count=1 -run 'TestCommitService_AlwaysOnPhaseLines|TestCommitService_VerboseIsSuperset' ./application/...` exits 0.

## Cross-cutting acceptance criteria

- After all six tasks complete, `make test` exits 0.
- `NewClient` signature change ripples to every construction site. The only known site is `cmd/commit.go:119` — update to pass `0, 0, nil` for the three new args (defaults take over; phase 4 / Batch 4 task #24 will properly thread the resolved config values).
- Existing openai tests that construct `Client` directly must add `0, 0, nil` to their call sites.
- No prohibited Definition-of-Done patterns: no stubs, no `TODO`/`FIXME`, no `NotImplemented`, no empty function bodies.
- Each new test was observed to FAIL before its paired implementation landed.
- The heartbeat goroutine does not leak (verified by `goleak`).
- No secret material in any stderr line: no API key, no base URL, no prompt body. (REQ-011 is covered by Batch 5's security task #26, but Batch 2's code must already satisfy it.)

## Sign-off

- **Acceptance criteria authoring:** auto-derived from task files; do NOT add new criteria.
- **Coordinator verdict:** PASS only if every acceptance criterion above is satisfied and the cross-cutting checks pass.
- **Rework budget:** maximum 2 evaluator-rework rounds before escalation.
- **Revision history:** initial revision.
