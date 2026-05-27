# Task 004 — OpenAI HTTP timeout (test)

| Field | Value |
|---|---|
| **subject** | Add unit test for per-attempt HTTP timeout in `callLLM` |
| **type** | test |
| **depends-on** | ["001-config-keys-impl"] |
| **REQ refs** | REQ-001 |
| **layer** | infrastructure/openai |

## Files to create / modify

- `infrastructure/openai/client_test.go` — add `TestClient_PerAttemptTimeout`
- `infrastructure/openai/internal/fakeserver_test.go` — new test-only stall server helper (or inline `httptest.NewServer`)

## BDD Scenario

```gherkin
Scenario: Per-attempt timeout fires three retries instead of hanging
  Given a fake LLM endpoint at http://127.0.0.1:18080 that accepts the TCP connection and writes no response body
  And the configured request_timeout is 5 seconds (lowered for the test)
  And the configured base_url is http://127.0.0.1:18080
  When I run "git-agent commit" against the fixture
  Then attempt 1 aborts after 5 seconds with context.DeadlineExceeded
  And attempt 2 aborts after 5 seconds with the same deadline
  And attempt 3 aborts after 5 seconds with the same deadline
  And the process exits within 17 seconds with exit code 1
  And stderr contains the literal substring "request timed out after 5s"
  And stderr names the model from the resolved config
  And stderr does not contain the strings "panic", "goroutine", or "context.DeadlineExceeded" raw
```

## Acceptance criteria

- Test starts a `httptest.NewServer` with a handler that hijacks the connection and never writes.
- Test constructs `openai.NewClient(apiKey, server.URL, model, 1*time.Second, 0, &buf)`.
- Test calls a method that invokes `callLLM` (e.g., `client.Generate(ctx, req)`) with a non-zero `MaxAttempts`-bounded context.
- Asserts the call returns within ~3.5 seconds total (3 × 1 s attempts + scheduling jitter).
- Asserts the returned error message contains `"request timed out after 1s"` and `"model=<configured>"`.
- Asserts the error message does NOT contain `"context.DeadlineExceeded"` raw or `"panic"`.

## Verification

```bash
go test -count=1 -run 'TestClient_PerAttemptTimeout' ./infrastructure/openai/...
```

Fails (RED) until task-004-impl lands.
