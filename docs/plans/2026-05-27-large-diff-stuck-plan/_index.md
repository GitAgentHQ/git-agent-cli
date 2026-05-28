# Plan: large-diff "stuck" remediation

Executable implementation plan derived from the design at
`docs/plans/2026-05-27-large-diff-stuck-design/`. Twelve paired
test/impl features plus two test-only invariant guards, covering the
14 BDD scenarios and 11 requirements (REQ-001…REQ-011) recorded in
the design.

## Context

The user reported `在修改量特别大的情况下，git-agent 会被卡住` and supplied
a concrete failure trace:

```
Error: plan commits: LLM exhausted token limit
       (model=deepseek-v4-flash, max_tokens=32768, attempt=3/3)
```

The design names four root causes that compound on large diffs (no
HTTP timeout, no signal handling, default-mode silence, unbounded
token-doubling) and adds an opt-in heuristic planner fallback for
chronically misbehaving models. This plan decomposes those five
locked branches into independently-testable tasks, following RED →
GREEN ordering per BDD/TDD discipline.

### Current-state vs target-state

| Dimension | Current | Target |
|---|---|---|
| LLM HTTP timeout | None (default `http.Client{}`) | Per-attempt `request_timeout` (default 90s) via `http.Client.Timeout` + `context.WithTimeout` |
| Signal handling | None — Ctrl-C kills the process via OS default action | `signal.NotifyContext` at `main.go`, `cmd.ExecuteContext(ctx)` propagates through `cmd.Context()` |
| Default-mode progress output | Silent (all logs gated by `--verbose`) | 9 always-on phase lines + 15s heartbeat tick during LLM waits |
| Token-doubling retry | Unbounded — `MaxCompletionTokens *= 2` for 3 attempts | Per-endpoint ceiling (16384 for plan/generate/scopes, 4096 for detect); typed `commit.PlannerBudgetExhaustedError` on overrun |
| Oversized single-file diff | Head-chopped to byte cap; LLM sees meaningless prefix | `DIFF-SYNOPSIS` block built from `git diff --stat`; hook still sees raw diff |
| Planner failure fallback | Hard error, no commit | Opt-in `plan_fallback: heuristic` → `directoryBucketer` produces directory-grouped commits |
| Error UX | `"max_tokens=32768, attempt=3/3"` (says what failed, not what to do) | `"model=X, ceiling=N; try a more capable model, --max-diff-lines, --intent, smaller batches"` |

### Constraints

- Clean Architecture preserved (cmd → application → domain ← infrastructure).
- `domain/commit` retains zero external imports.
- `go test -count=1 ./...` must pass after every batch.
- No new top-level dependencies (one allowed addition: `go.uber.org/goleak` for heartbeat-leak detection, added via `go get`).
- Default behaviour for users who do nothing: REQ-001/REQ-004 active (timeout + heartbeat with sane defaults); REQ-008 inactive (`plan_fallback` defaults to `none`).
- No emojis in code, comments, error messages, or this plan.

## Execution Plan

```yaml
tasks:
  - id: "001-config-keys-test"
    subject: "Config keys (test)"
    slug: "config-keys-test"
    type: "test"
    depends-on: []
  - id: "001-config-keys-impl"
    subject: "Config keys (impl)"
    slug: "config-keys-impl"
    type: "impl"
    depends-on: ["001-config-keys-test"]

  - id: "002-domain-errors-test"
    subject: "Domain errors (test)"
    slug: "domain-errors-test"
    type: "test"
    depends-on: []
  - id: "002-domain-errors-impl"
    subject: "Domain errors (impl)"
    slug: "domain-errors-impl"
    type: "impl"
    depends-on: ["002-domain-errors-test"]

  - id: "003-git-stagediffstat-test"
    subject: "Git StagedDiffStat (test)"
    slug: "git-stagediffstat-test"
    type: "test"
    depends-on: []
  - id: "003-git-stagediffstat-impl"
    subject: "Git StagedDiffStat (impl)"
    slug: "git-stagediffstat-impl"
    type: "impl"
    depends-on: ["003-git-stagediffstat-test"]

  - id: "004-openai-http-timeout-test"
    subject: "OpenAI HTTP timeout (test)"
    slug: "openai-http-timeout-test"
    type: "test"
    depends-on: ["001-config-keys-impl"]
  - id: "004-openai-http-timeout-impl"
    subject: "OpenAI HTTP timeout (impl)"
    slug: "openai-http-timeout-impl"
    type: "impl"
    depends-on: ["004-openai-http-timeout-test"]

  - id: "005-openai-heartbeat-test"
    subject: "OpenAI heartbeat (test)"
    slug: "openai-heartbeat-test"
    type: "test"
    depends-on: ["001-config-keys-impl"]
  - id: "005-openai-heartbeat-impl"
    subject: "OpenAI heartbeat (impl)"
    slug: "openai-heartbeat-impl"
    type: "impl"
    depends-on: ["005-openai-heartbeat-test"]

  - id: "006-openai-token-ceiling-test"
    subject: "OpenAI token ceiling (test)"
    slug: "openai-token-ceiling-test"
    type: "test"
    depends-on: ["002-domain-errors-impl"]
  - id: "006-openai-token-ceiling-impl"
    subject: "OpenAI token ceiling (impl)"
    slug: "openai-token-ceiling-impl"
    type: "impl"
    depends-on: ["006-openai-token-ceiling-test"]

  - id: "007-cmd-signal-test"
    subject: "Cmd SIGINT cancellation (test)"
    slug: "cmd-signal-test"
    type: "test"
    depends-on: ["004-openai-http-timeout-impl"]
  - id: "007-cmd-signal-impl"
    subject: "Cmd SIGINT cancellation (impl)"
    slug: "cmd-signal-impl"
    type: "impl"
    depends-on: ["007-cmd-signal-test"]

  - id: "008-app-phase-output-test"
    subject: "Application phase output (test)"
    slug: "app-phase-output-test"
    type: "test"
    depends-on: []
  - id: "008-app-phase-output-impl"
    subject: "Application phase output (impl)"
    slug: "app-phase-output-impl"
    type: "impl"
    depends-on: ["008-app-phase-output-test"]

  - id: "009-app-synopsis-fallback-test"
    subject: "Application DIFF-SYNOPSIS fallback (test)"
    slug: "app-synopsis-fallback-test"
    type: "test"
    depends-on: ["003-git-stagediffstat-impl"]
  - id: "009-app-synopsis-fallback-impl"
    subject: "Application DIFF-SYNOPSIS fallback (impl)"
    slug: "app-synopsis-fallback-impl"
    type: "impl"
    depends-on: ["009-app-synopsis-fallback-test"]

  - id: "010-app-heuristic-fallback-test"
    subject: "Application heuristic fallback (test)"
    slug: "app-heuristic-fallback-test"
    type: "test"
    depends-on: ["002-domain-errors-impl"]
  - id: "010-app-heuristic-fallback-impl"
    subject: "Application heuristic fallback (impl)"
    slug: "app-heuristic-fallback-impl"
    type: "impl"
    depends-on: ["010-app-heuristic-fallback-test"]

  - id: "011-cmd-budget-error-render-test"
    subject: "Cmd budget-error rendering (test)"
    slug: "cmd-budget-error-render-test"
    type: "test"
    depends-on: ["002-domain-errors-impl", "006-openai-token-ceiling-impl"]
  - id: "011-cmd-budget-error-render-impl"
    subject: "Cmd budget-error rendering (impl)"
    slug: "cmd-budget-error-render-impl"
    type: "impl"
    depends-on: ["011-cmd-budget-error-render-test"]

  - id: "012-cmd-wire-config-test"
    subject: "Cmd config wiring (test)"
    slug: "cmd-wire-config-test"
    type: "test"
    depends-on: ["001-config-keys-impl", "004-openai-http-timeout-impl", "005-openai-heartbeat-impl", "010-app-heuristic-fallback-impl"]
  - id: "012-cmd-wire-config-impl"
    subject: "Cmd config wiring (impl)"
    slug: "cmd-wire-config-impl"
    type: "impl"
    depends-on: ["012-cmd-wire-config-test"]

  - id: "013-regression-test"
    subject: "Regression (test-only)"
    slug: "regression-test"
    type: "test"
    depends-on: ["004-openai-http-timeout-impl", "005-openai-heartbeat-impl", "006-openai-token-ceiling-impl", "007-cmd-signal-impl", "008-app-phase-output-impl", "009-app-synopsis-fallback-impl", "010-app-heuristic-fallback-impl", "011-cmd-budget-error-render-impl", "012-cmd-wire-config-impl"]

  - id: "014-security-test"
    subject: "Security (test-only)"
    slug: "security-test"
    type: "test"
    depends-on: ["005-openai-heartbeat-impl", "008-app-phase-output-impl"]
```

## Task File References

Foundation:
- [Task 001 — Config keys (test)](./task-001-config-keys-test.md)
- [Task 001 — Config keys (impl)](./task-001-config-keys-impl.md)
- [Task 002 — Domain errors (test)](./task-002-domain-errors-test.md)
- [Task 002 — Domain errors (impl)](./task-002-domain-errors-impl.md)
- [Task 003 — Git StagedDiffStat (test)](./task-003-git-stagediffstat-test.md)
- [Task 003 — Git StagedDiffStat (impl)](./task-003-git-stagediffstat-impl.md)

LLM client (infrastructure/openai/):
- [Task 004 — OpenAI HTTP timeout (test)](./task-004-openai-http-timeout-test.md)
- [Task 004 — OpenAI HTTP timeout (impl)](./task-004-openai-http-timeout-impl.md)
- [Task 005 — OpenAI heartbeat (test)](./task-005-openai-heartbeat-test.md)
- [Task 005 — OpenAI heartbeat (impl)](./task-005-openai-heartbeat-impl.md)
- [Task 006 — OpenAI token ceiling (test)](./task-006-openai-token-ceiling-test.md)
- [Task 006 — OpenAI token ceiling (impl)](./task-006-openai-token-ceiling-impl.md)

Signal handling:
- [Task 007 — Cmd SIGINT cancellation (test)](./task-007-cmd-signal-test.md)
- [Task 007 — Cmd SIGINT cancellation (impl)](./task-007-cmd-signal-impl.md)

Application layer:
- [Task 008 — Application phase output (test)](./task-008-app-phase-output-test.md)
- [Task 008 — Application phase output (impl)](./task-008-app-phase-output-impl.md)
- [Task 009 — Application DIFF-SYNOPSIS fallback (test)](./task-009-app-synopsis-fallback-test.md)
- [Task 009 — Application DIFF-SYNOPSIS fallback (impl)](./task-009-app-synopsis-fallback-impl.md)
- [Task 010 — Application heuristic fallback (test)](./task-010-app-heuristic-fallback-test.md)
- [Task 010 — Application heuristic fallback (impl)](./task-010-app-heuristic-fallback-impl.md)

Cmd-layer wiring & error rendering:
- [Task 011 — Cmd budget-error rendering (test)](./task-011-cmd-budget-error-render-test.md)
- [Task 011 — Cmd budget-error rendering (impl)](./task-011-cmd-budget-error-render-impl.md)
- [Task 012 — Cmd config wiring (test)](./task-012-cmd-wire-config-test.md)
- [Task 012 — Cmd config wiring (impl)](./task-012-cmd-wire-config-impl.md)

Regression & security guards:
- [Task 013 — Regression (test-only)](./task-013-regression-test.md)
- [Task 014 — Security (test-only)](./task-014-security-test.md)

## BDD Coverage

All 14 scenarios from `docs/plans/2026-05-27-large-diff-stuck-design/bdd-specs.md`
are covered by at least one test task.

| Scenario (bdd-specs.md line) | REQ | Covered by |
|---|---|---|
| Per-attempt timeout fires three retries instead of hanging (`:21`) | REQ-001 | task-004-openai-http-timeout-test |
| SIGINT during an LLM call cancels within one second and re-stages (`:35`) | REQ-002 | task-007-cmd-signal-test |
| Default-mode commit prints recognizable phase lines on stderr (`:64`) | REQ-003 | task-008-app-phase-output-test |
| A 47-second LLM call emits two heartbeat ticks (`:76`) | REQ-004 | task-005-openai-heartbeat-test |
| --verbose output is a strict superset of always-on output (`:88`) | REQ-010 | task-008-app-phase-output-test |
| Planner doubling halts at the ceiling after one attempted double (`:112`) | REQ-005 | task-006-openai-token-ceiling-test |
| Budget-exhausted error names the model and at least two remediations (`:121`) | REQ-006 | task-011-cmd-budget-error-render-test |
| Single-file 1 MB diff triggers the DIFF-SYNOPSIS fallback (`:144`) | REQ-007 | task-009-app-synopsis-fallback-test |
| Multi-file group with oversized total diff stays on the truncator path (`:159`) | REQ-007 | task-009-app-synopsis-fallback-test |
| Planner budget exhaustion triggers directory bucketing (`:183`) | REQ-008 | task-010-app-heuristic-fallback-test |
| With plan_fallback=none the existing hard-error path is preserved (`:196`) | REQ-008 | task-010-app-heuristic-fallback-test |
| Existing small-diff happy path is unchanged (`:214`) | REQ-009 | task-013-regression-test |
| go test ./... passes after the redesign (`:224`) | REQ-009 | task-013-regression-test |
| Heartbeat and phase lines emit only metadata (`:231`) | REQ-011 | task-014-security-test |

## Dependency Chain

The graph is acyclic. Six chains can run in parallel; they converge at
tasks 011 / 012 / 013. Foundation tasks (001 / 002 / 003) have no
dependencies and can be the first parallel batch.

```
Batch 1 (no deps — fully parallel):
  001-config-keys-test
  002-domain-errors-test
  003-git-stagediffstat-test
  008-app-phase-output-test

Batch 2 (Red → Green pairs of Batch 1):
  001-config-keys-impl    (depends-on 001-test)
  002-domain-errors-impl  (depends-on 002-test)
  003-git-stagediffstat-impl (depends-on 003-test)
  008-app-phase-output-impl  (depends-on 008-test)

Batch 3 (deps on Batch 2 foundation impls):
  004-openai-http-timeout-test  (depends-on 001-impl)
  005-openai-heartbeat-test     (depends-on 001-impl)
  006-openai-token-ceiling-test (depends-on 002-impl)
  009-app-synopsis-fallback-test (depends-on 003-impl)
  010-app-heuristic-fallback-test (depends-on 002-impl)

Batch 4 (Red → Green pairs of Batch 3):
  004-openai-http-timeout-impl
  005-openai-heartbeat-impl
  006-openai-token-ceiling-impl
  009-app-synopsis-fallback-impl
  010-app-heuristic-fallback-impl

Batch 5 (cross-layer wiring + signal):
  007-cmd-signal-test           (depends-on 004-impl)
  011-cmd-budget-error-render-test (depends-on 002-impl, 006-impl)

Batch 6 (Red → Green pairs of Batch 5):
  007-cmd-signal-impl
  011-cmd-budget-error-render-impl

Batch 7 (config wiring — last to merge):
  012-cmd-wire-config-test (depends-on 001-impl, 004-impl, 005-impl, 010-impl)

Batch 8 (Red → Green pair of Batch 7):
  012-cmd-wire-config-impl

Batch 9 (guards — depend on all impls):
  013-regression-test (depends-on 004/005/006/007/008/009/010/011/012-impl)
  014-security-test   (depends-on 005-impl, 008-impl)
```

Ascii dependency graph (arrows point from prerequisite to dependent):

```
                          [001-test]    [002-test]    [003-test]    [008-test]
                              |             |             |             |
                              v             v             v             v
                          [001-impl]    [002-impl]    [003-impl]    [008-impl]
                              |             |             |             |
                              +-----+-------+--+----------+             |
                              |     |          |                        |
                              v     v          v                        |
                      [004-test] [005-test] [006-test]                  |
                              |     |          |                        |
                              v     v          v                        |
                      [004-impl] [005-impl] [006-impl]                  |
                              |     |          |                        |
                              v     v          v                        |
                                                                        |
                          [009-test] (003-impl)    [010-test] (002-impl)
                              |                        |
                              v                        v
                          [009-impl]               [010-impl]
                              |                        |
                              +-----+------------------+----+
                                    |                       |
                                    v                       v
                              [007-test] (004-impl)   [011-test] (002+006-impl)
                                    |                       |
                                    v                       v
                              [007-impl]              [011-impl]
                                                            |
                                                            v
                                              [012-test] (001+004+005+010-impl)
                                                            |
                                                            v
                                                      [012-impl]
                                                            |
                                                            v
                                              [013-test] (4,5,6,7,8,9,10,11,12-impl)
                                              [014-test] (005-impl, 008-impl)
```
