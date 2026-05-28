# Task 012 — Cmd config wiring (impl)

| Field | Value |
|---|---|
| **subject** | Thread `request_timeout`, `heartbeat_interval`, `plan_fallback` from resolved config into constructors |
| **type** | impl |
| **depends-on** | ["012-cmd-wire-config-test"] |
| **REQ refs** | REQ-001, REQ-004, REQ-008 (wiring layer) |
| **layer** | cmd |

## Files to modify

- `cmd/commit.go` — extend `runCommit` to pass the three resolved values into `infraOpenAI.NewClient` and `application.NewCommitService`

## Implementation steps

1. After `providerCfg, err := resolveProviderConfig(cmd)` (~line 49), read `providerCfg.RequestTimeout` and `providerCfg.HeartbeatInterval`.
2. Pass both into the `infraOpenAI.NewClient(...)` call (~line 119) alongside a writer (use `cmd.ErrOrStderr()` to surface heartbeat lines to stderr).
3. Read `projCfg.PlanFallback`. When `== project.PlanFallbackHeuristic`, construct `heuristicPlanner := application.NewDirectoryBucketer()`; else `nil`.
4. Pass `heuristicPlanner` into `application.NewCommitService(...)` as the new last argument.
5. Verify the existing `--free` path still works (provider config resolution unchanged).

## Verification

```bash
go test -count=1 ./cmd/... ./e2e/...
```

Task-012-test passes. Existing cmd / e2e tests stay green.
