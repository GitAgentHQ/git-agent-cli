# Task 007 — Cmd SIGINT/SIGTERM cancellation (impl)

| Field | Value |
|---|---|
| **subject** | Wire `signal.NotifyContext` in `main.go`; expose `cmd.ExecuteContext` |
| **type** | impl |
| **depends-on** | ["007-cmd-signal-test"] |
| **REQ refs** | REQ-002 |
| **layer** | cmd / main |

## Files to modify

- `main.go` — rewrite body
- `cmd/root.go` — add `ExecuteContext(ctx context.Context)` alongside existing `Execute()`

## Interface contracts

```go
// main.go — entry-point rewrite per architecture.md §2.1
func main()

// cmd/root.go — new entry alongside existing Execute()
func ExecuteContext(ctx context.Context)
```

Concrete bodies are documented in `docs/plans/2026-05-27-large-diff-stuck-design/architecture.md` §2.1 (signal wiring) — see that document for the four-line `signal.NotifyContext` pattern and the `ExecuteContext` wrapping requirement. Do not duplicate the body here.

## Implementation steps

1. Rewrite `main.go` to the four-line context pattern.
2. Add `ExecuteContext(ctx context.Context)` to `cmd/root.go`; refactor the existing `Execute()` to share the exit-code mapping via a private helper.
3. Verify cmd-layer error-path map handles `context.Canceled`: convert to a clean stderr line `"cancelled"` and non-zero exit (exit code 1). This may require a new switch arm in `cmd/commit.go` error handling — add minimally.
4. The existing `defer` in `application/commit_service.go:347-361` already uses `context.Background()` for recovery; verify it still re-stages after SIGINT (this is a manual check; the e2e test enforces it).

## Verification

```bash
go test -count=1 ./e2e/... ./cmd/...
```

Task-007-test passes. Existing tests stay green.
