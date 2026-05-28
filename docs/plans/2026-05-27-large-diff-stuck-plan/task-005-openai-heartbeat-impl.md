# Task 005 — OpenAI heartbeat (impl)

| Field | Value |
|---|---|
| **subject** | Spawn per-attempt heartbeat goroutine inside `callLLM` |
| **type** | impl |
| **depends-on** | ["005-openai-heartbeat-test"] |
| **REQ refs** | REQ-004 |
| **layer** | infrastructure/openai |

## Files to modify

- `infrastructure/openai/client.go` — add `heartbeat` private method and per-attempt spawn

## Interface contracts

```go
// On Client struct (extends task-004's signature):
type Client struct {
    // ...fields from task-004...
    heartbeatInterval time.Duration
}

// Private method
func (c *Client) heartbeat(ctx context.Context, done <-chan struct{})
```

Lifecycle: caller spawns with `go c.heartbeat(attemptCtx, done)`; closes `done` after `CreateChatCompletion` returns. Goroutine exits within 100 ms via select on `done`, `ctx.Done()`, or next ticker tick.

## Implementation steps

1. Add `heartbeatInterval` field on `Client`; populate in `NewClient` from the param (default 15s when ≤0).
2. Add `heartbeat(ctx, done)` private method following the `time.NewTicker` + `defer Stop()` pattern in `architecture.md` §2.3.
3. In `callLLM`, before each `CreateChatCompletion`, create `done := make(chan struct{})` and `go c.heartbeat(attemptCtx, done)`.
4. After the call returns (any outcome), `close(done)` before `cancel()`.
5. Heartbeat line format: `"still waiting on LLM... (%ds elapsed, model=%s)\n"` via `fmt.Fprintf(c.out, ...)`.
6. When `c.out == nil`, the goroutine exits immediately — no-op heartbeat for tests / contexts without a writer.

## Verification

```bash
go test -count=1 ./infrastructure/openai/...
```

Task-005-test passes. `goleak` reports no leaked goroutines.
