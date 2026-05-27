# Task 003 — Git `StagedDiffStat` (impl)

| Field | Value |
|---|---|
| **subject** | Add `StagedDiffStat(ctx) (string, error)` to git client |
| **type** | impl |
| **depends-on** | ["003-git-stagediffstat-test"] |
| **REQ refs** | REQ-007 (prerequisite) |
| **layer** | infrastructure/git |

## Files to modify

- `infrastructure/git/client.go` — add method
- `application/commit_service.go` — extend `CommitGitClient` interface
- `application/commit_service_test.go` (and other test files with fakes) — add `StagedDiffStat` stub returning `("", nil)` on each fake

## Interface contracts

```go
// infrastructure/git/client.go
func (c *Client) StagedDiffStat(ctx context.Context) (string, error)

// application/commit_service.go (extend existing interface)
type CommitGitClient interface {
    // ...existing methods...
    StagedDiffStat(ctx context.Context) (string, error)
}
```

## Implementation steps

1. In `client.go`, add `StagedDiffStat` next to `StagedDiff`. Run `git diff --staged --stat --ignore-submodules=all` via `exec.CommandContext`. Return stdout as string.
2. Add the method signature to the `CommitGitClient` interface in `commit_service.go`.
3. Update every fake implementing the interface (`application/*_test.go`) to add the stub.

## Verification

```bash
go test -count=1 ./infrastructure/git/... ./application/...
```

Task-003-test passes. Existing application tests stay green (fake stubs are inert).
