# Task 007 — Cmd SIGINT/SIGTERM cancellation (test)

| Field | Value |
|---|---|
| **subject** | Add e2e test asserting SIGINT cancels in-flight LLM call and re-stages files |
| **type** | test |
| **depends-on** | ["004-openai-http-timeout-impl"] |
| **REQ refs** | REQ-002 |
| **layer** | cmd / e2e |

## Files to modify

- `e2e/commit_test.go` — add `TestCommitCmd_SIGINTCancels`
- `e2e/helpers_test.go` — add `newFakeLLMServer` helper (stall handler variant) if not present

## BDD Scenario

```gherkin
Scenario: SIGINT during an LLM call cancels within one second and re-stages
  Given the user pre-staged the files "a.go" and "b.go" via "git add a.go b.go"
  And the working tree also contains an unstaged file "c.go"
  And a fake LLM endpoint at http://127.0.0.1:18080 that holds the response open for 600 seconds
  And the configured base_url is http://127.0.0.1:18080
  When I run "git-agent commit" and send SIGINT to its PID after the heartbeat tick at second 15 is observed
  Then the in-flight HTTP request is cancelled within 1 second of the signal
  And no commit is created
  And "git diff --staged --name-only" lists "a.go" and "b.go" exactly
  And "c.go" remains unstaged
  And the process exits with a non-zero exit code
  And stderr contains the literal substring "cancelled"
  And stderr does not contain a Go panic stack trace
```

## Acceptance criteria

- Test starts a stall server (handler reads request then blocks on `<-r.Context().Done()`).
- Test creates a temp repo with three files; pre-stages `a.go` and `b.go`.
- Test launches `git-agent commit` as a subprocess (existing `e2e/helpers_test.go` pattern); waits 200 ms for the call to start; sends `cmd.Process.Signal(os.Interrupt)`.
- Asserts process exits within 1 second of the signal with non-zero exit code.
- Asserts `git diff --staged --name-only` lists exactly `a.go` and `b.go` (the original pre-stage state).
- Asserts stderr contains `"cancelled"` and does NOT contain `"panic"` or `"goroutine"`.

## Verification

```bash
go test -count=1 -run 'TestCommitCmd_SIGINTCancels' ./e2e/...
```

Fails (RED) until task-007-impl lands.
