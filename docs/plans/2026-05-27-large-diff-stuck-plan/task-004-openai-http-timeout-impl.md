# Task 004 — OpenAI HTTP timeout (impl)

| Field | Value |
|---|---|
| **subject** | Wire per-attempt HTTP timeout via `http.Client.Timeout` + `context.WithTimeout` |
| **type** | impl |
| **depends-on** | ["004-openai-http-timeout-test"] |
| **REQ refs** | REQ-001 |
| **layer** | infrastructure/openai |

## Files to modify

- `infrastructure/openai/client.go` — extend `NewClient` signature, set `cfg.HTTPClient`, wrap each `CreateChatCompletion` attempt with `context.WithTimeout`, classify `context.DeadlineExceeded` into the actionable error.

## Interface contracts

```go
func NewClient(
    apiKey, baseURL, model string,
    requestTimeout, heartbeatInterval time.Duration,
    out io.Writer,
) *Client
```

Default fallback: when `requestTimeout <= 0`, use `90 * time.Second`. Stored on `Client` struct as `requestTimeout` field.

## Implementation steps

1. Extend `Client` struct with `requestTimeout time.Duration` and `out io.Writer` fields.
2. Extend `NewClient` signature; populate defaults; set `cfg.HTTPClient = &http.Client{Timeout: requestTimeout}`.
3. In `callLLM`, wrap each `CreateChatCompletion` call: `attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout); defer cancel()`. Pass `attemptCtx`.
4. After the call, distinguish `context.DeadlineExceeded` (attempt timeout, retry) from `context.Canceled` (SIGINT, return immediately).
5. On `DeadlineExceeded`, set `lastErr` to a formatted message `"request timed out after %s (model=%s, attempt=%d/%d)"` and `continue` the retry loop.
6. Update the four `callLLM` call sites in the file (`Generate`, `Plan`, `DetectTechnologies`, `GenerateScopes`) to also pass `0` for the new ceiling argument introduced by task-006 — leave a TODO comment for the integrator if task-006 hasn't merged yet; otherwise pass the per-endpoint ceiling.

## Verification

```bash
go test -count=1 ./infrastructure/openai/...
```

Task-004-test passes. Existing openai tests stay green.
