# Task 012 — Cmd config wiring (test)

| Field | Value |
|---|---|
| **subject** | Add unit test verifying `request_timeout`, `heartbeat_interval`, `plan_fallback` flow from config to constructors |
| **type** | test |
| **depends-on** | ["001-config-keys-impl", "004-openai-http-timeout-impl", "005-openai-heartbeat-impl", "010-app-heuristic-fallback-impl"] |
| **REQ refs** | REQ-001, REQ-004, REQ-008 (wiring layer) |
| **layer** | cmd |

## Files to modify

- `cmd/commit_test.go` — add `TestCommit_WiresConfigToConstructors`

## BDD Coverage

Wiring task — no standalone Scenario; ensures `_index.md` REQ-001/REQ-004/REQ-008 manifest in the binary. The Scenarios for those REQs (`Per-attempt timeout fires three retries instead of hanging`, `A 47-second LLM call emits two heartbeat ticks`, `Planner budget exhaustion triggers directory bucketing`) all assume wiring is correct; this task pins it down with a test.

## Acceptance criteria

- Test loads a user-config YAML fixture with `request_timeout: 5s`, `heartbeat_interval: 2s`.
- Test loads a project-config YAML fixture with `plan_fallback: heuristic`.
- Test invokes the constructor-resolution path in `runCommit` (refactored into a helper for testability if necessary).
- Asserts the resulting `openai.Client` reports `requestTimeout == 5*time.Second`, `heartbeatInterval == 2*time.Second` (via test-only accessors or capture-via-spy NewClient).
- Asserts the `CommitService` is constructed with a non-nil `heuristicPlanner` when `PlanFallback == "heuristic"`, and nil when `PlanFallback == "none"`.

## Verification

```bash
go test -count=1 -run 'TestCommit_WiresConfigToConstructors' ./cmd/...
```

Fails (RED) until task-012-impl lands.
