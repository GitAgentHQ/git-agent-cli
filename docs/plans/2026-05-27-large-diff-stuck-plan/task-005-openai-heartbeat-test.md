# Task 005 — OpenAI heartbeat (test)

| Field | Value |
|---|---|
| **subject** | Add unit test for 15s heartbeat tick during in-flight `callLLM` |
| **type** | test |
| **depends-on** | ["001-config-keys-impl"] |
| **REQ refs** | REQ-004 |
| **layer** | infrastructure/openai |

## Files to modify

- `infrastructure/openai/client_test.go` — add `TestClient_HeartbeatTicks`
- `infrastructure/openai/client_test.go` — add `goleak.VerifyTestMain(m)` if not present
- `go.mod` — add `go.uber.org/goleak` (use `go get`, never hand-edit)

## BDD Scenario

```gherkin
Scenario: A 47-second LLM call emits two heartbeat ticks
  Given a fake LLM endpoint that holds the planner response open for 47 seconds before sending JSON
  And the configured heartbeat_interval is 15 seconds
  When I run "git-agent commit" against the fixture
  Then stderr contains exactly 2 lines matching "^still waiting on LLM... \([0-9]+s elapsed"
  And the first tick line reports elapsed seconds in the closed range [15, 17]
  And the second tick line reports elapsed seconds in the closed range [30, 32]
  And each tick line names the model from the resolved config
  And the heartbeat goroutine stops within 100 ms of the planner response completing
  And the test does not report a leaked goroutine via goleak
```

## Acceptance criteria

- Test starts a `httptest.NewServer` whose handler `time.Sleep`s for a controllable duration (parameter), then responds 200 with canned JSON. For test speed, use `heartbeat_interval=100ms`, `sleep=350ms` so the test sees 3 ticks in <1 s.
- Test captures stderr via `bytes.Buffer` passed as the `out io.Writer`.
- Asserts the buffer contains exactly N tick lines matching the regex `^still waiting on LLM... \(\d+s elapsed, model=`.
- Asserts the model name from the constructor appears in each tick line.
- `goleak.VerifyTestMain` (or per-test `goleak.VerifyNone(t)`) catches a heartbeat goroutine that fails to exit.

## Verification

```bash
go test -count=1 -run 'TestClient_HeartbeatTicks' ./infrastructure/openai/...
```

Fails (RED) until task-005-impl lands.
