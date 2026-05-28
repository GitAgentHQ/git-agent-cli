# Task 010 — Application heuristic planner fallback (impl)

| Field | Value |
|---|---|
| **subject** | Add `HeuristicPlanner` interface + `directoryBucketer` impl; wire fallback in `CommitService` |
| **type** | impl |
| **depends-on** | ["010-app-heuristic-fallback-test"] |
| **REQ refs** | REQ-008 |
| **layer** | domain/commit + application |

## Files to create / modify

- `domain/commit/heuristic_planner.go` (new) — `HeuristicPlanner` interface
- `application/heuristic_planner.go` (new) — `directoryBucketer` impl + `NewDirectoryBucketer()` constructor
- `application/commit_service.go` — add `heuristicPlanner commit.HeuristicPlanner` field; extend `NewCommitService` signature; insert fallback branch around `Plan` call sites

## Interface contracts

```go
// domain/commit/heuristic_planner.go
package commit

import "context"

type HeuristicPlanner interface {
    Plan(ctx context.Context, req PlanRequest) (*CommitPlan, error)
}
```

```go
// application/heuristic_planner.go
type directoryBucketer struct{}

func NewDirectoryBucketer() commit.HeuristicPlanner { /* ... */ }

func (b *directoryBucketer) Plan(ctx context.Context, req commit.PlanRequest) (*commit.CommitPlan, error)
```

```go
// application/commit_service.go
func NewCommitService(
    gen commit.CommitMessageGenerator,
    planner commit.CommitPlanner,
    git CommitGitClient,
    hookExec hook.HookExecutor,
    scopeSvc *ScopeService,
    filter diff.DiffFilter,
    truncator diff.DiffTruncator,
    heuristicPlanner commit.HeuristicPlanner, // new
) *CommitService
```

## Implementation steps

1. Add `domain/commit/heuristic_planner.go` with the interface declaration.
2. Add `application/heuristic_planner.go` implementing `directoryBucketer`:
   - Bucket files by first path component (`strings.SplitN(file, "/", 2)[0]`).
   - When `req.Config.Scopes` non-empty, map each top-level dir to the scope whose description contains the dir name (case-insensitive substring); unmapped dirs use empty scope.
   - Cap at `maxCommitGroups` (5) — merge smallest buckets into the last group.
   - Title placeholder: `"chore(<scope>): update N files in <dir>/"` (scoped) or `"chore: update N files in <dir>/"` (unscoped).
3. Extend `CommitService` struct with `heuristicPlanner` field; extend `NewCommitService` with new last parameter.
4. Wrap both `Plan` call sites (~lines 275-283 and ~312-320) in the fallback pattern from `architecture.md` §2.2: on `errors.Is(err, commit.ErrPlannerBudgetExhausted)` and `PlanFallback == "heuristic"` and non-nil `heuristicPlanner`, retry via `s.heuristicPlanner.Plan(ctx, planReq)`.
5. Emit `s.out(req, "planner exhausted budget — falling back to directoryBucketer")` on the fallback path.

## Verification

```bash
go test -count=1 ./application/... ./domain/commit/...
```

Task-010-test passes. `domain/commit` retains zero external imports.
