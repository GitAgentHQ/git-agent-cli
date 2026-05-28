# Task 001 — Config keys (impl)

| Field | Value |
|---|---|
| **subject** | Register `request_timeout`, `heartbeat_interval`, `plan_fallback` in the config registry + structs |
| **type** | impl |
| **depends-on** | ["001-config-keys-test"] |
| **REQ refs** | REQ-001, REQ-004, REQ-008 |
| **layer** | infrastructure/config |

## Files to modify

- `infrastructure/config/keys.go` — three new registry entries + three kebab aliases
- `infrastructure/config/resolver.go` — extend `ProviderConfig`, add defaults, extend `Resolve`
- `infrastructure/config/project.go` — extend `project.Config` with `PlanFallback`

## Interface contracts

```go
// resolver.go
type ProviderConfig struct {
    // ...existing fields...
    RequestTimeout    time.Duration
    HeartbeatInterval time.Duration
}

const (
    DefaultRequestTimeout    = 90 * time.Second
    DefaultHeartbeatInterval = 15 * time.Second
)
```

```go
// project.go (extend existing struct)
type Config struct {
    // ...existing fields...
    PlanFallback string `yaml:"plan_fallback,omitempty"` // "none" | "heuristic"
}

const (
    PlanFallbackNone      = "none"
    PlanFallbackHeuristic = "heuristic"
)
```

## Implementation steps

1. Add three entries to the `Keys` map in `keys.go` (`request_timeout`, `heartbeat_interval`, `plan_fallback`) with appropriate `AllowUser` / `AllowProject` / `AllowLocal` flags from `architecture.md` §2.5.
2. Add three kebab aliases in `KeyAliases`.
3. Add the two duration fields and two `Default*` constants in `resolver.go`; extend `Resolve` to populate them from precedence chain (flag → user config → default).
4. Add `PlanFallback` to `project.Config` plus two string constants.

## Verification

```bash
go test -count=1 ./infrastructure/config/...
```

All task-001-test cases turn green.
