# Task 001 — Config keys (test)

| Field | Value |
|---|---|
| **subject** | Add unit tests for `request_timeout`, `heartbeat_interval`, `plan_fallback` config keys |
| **type** | test |
| **depends-on** | [] |
| **REQ refs** | REQ-001, REQ-004, REQ-008 |
| **layer** | infrastructure/config |

## Files to create / modify

- `infrastructure/config/resolver_test.go` — extend with cases for the three new keys
- `infrastructure/config/project_test.go` — extend with `plan_fallback` round-trip
- `infrastructure/config/keys.go` — assert registry entries (read-only assertions in `keys_test.go` if it exists; create otherwise)

## BDD Coverage

Foundation task — exercises the schema surface required by REQ-001 (`request_timeout`), REQ-004 (`heartbeat_interval`), and REQ-008 (`plan_fallback`). No standalone Scenario from `bdd-specs.md`; the assertions cover the data contract these scenarios rely on.

## Acceptance criteria

- Test asserts `request_timeout` is registered as a user-scope-only key (no AllowProject / AllowLocal), Type `duration`, with `request-timeout` kebab alias.
- Test asserts `heartbeat_interval` is registered as a user-scope-only key, Type `duration`, with `heartbeat-interval` kebab alias.
- Test asserts `plan_fallback` is registered as a project-scope key with `plan-fallback` kebab alias.
- Test asserts `infraConfig.Resolve(...)` populates `ProviderConfig.RequestTimeout` and `ProviderConfig.HeartbeatInterval` from a YAML fixture, falling back to defaults `90s` / `15s` when absent.
- Test asserts `project.Config.PlanFallback` round-trips through YAML marshal / unmarshal with values `"none"` and `"heuristic"`; absent value yields zero string.

## Verification

```bash
go test -count=1 -run 'TestKeys|TestResolve|TestProjectConfig' ./infrastructure/config/...
```

All new test cases fail at the start of work (RED). They turn green only after task-001-impl lands.
