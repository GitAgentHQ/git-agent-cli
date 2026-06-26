# Task 017: Action-to-commit linking

**depends-on**: 012, 010

## Description
After `git.Commit()` succeeds in `CommitService`, find unlinked actions that modified the same files as the committed changes and link them via the `action_produces` table. This connects the action tracking pipeline to the commit flow, enabling timeline queries to show which actions contributed to each commit. Graceful: if no `graph.db` exists or any error occurs, the commit proceeds normally.

## Execution Context
**Task Number**: 017 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Capture service (012) and commit co-change enhancement (010) must be complete

## BDD Scenario
```gherkin
Feature: Action-to-commit linking
    As a developer
    I want actions automatically linked to the commits they produce
    So that the timeline shows the full action-to-commit chain

    Background:
        Given a git repository with an indexed graph database
        And the capture service has recorded actions

    Scenario: Actions linked to resulting commit
        Given a session "s1" with source "claude-code"
        And the following unlinked actions exist:
            | action | tool  | files_changed    |
            | s1:1   | Edit  | src/main.go      |
            | s1:2   | Write | src/main_test.go |
            | s1:3   | Bash  |                  |
        When CommitService commits files ["src/main.go", "src/main_test.go"]
        And the commit hash is "abc123"
        Then action_produces rows should link "s1:1" to "abc123"
        And action_produces rows should link "s1:2" to "abc123"
        And "s1:3" should NOT be linked (no file overlap)

    Scenario: No graph DB -- commit proceeds normally
        Given no .git-agent/graph.db exists
        When CommitService commits files
        Then the commit should succeed
        And no errors should be raised
        And no action_produces linking should be attempted

    Scenario: Graph DB error -- commit proceeds normally
        Given .git-agent/graph.db exists but is corrupted
        When CommitService commits files
        Then the commit should succeed
        And the linking error should be logged to verbose output
        And no error should propagate to the caller

    Scenario: No matching unlinked actions
        Given no actions have modified the committed files
        When CommitService commits files
        Then the commit should succeed
        And no action_produces rows should be created
```

## Files to Modify/Create
- `application/commit_service.go` -- modify to add post-commit linking

## Steps
### Step 1: Add optional GraphRepository to CommitService
Add an optional `graph.GraphRepository` field to `CommitService`. If nil, linking is skipped entirely.

```go
type CommitService struct {
    git       CommitGitClient
    llm       commit.Planner
    hookExec  hook.Executor
    graphRepo graph.GraphRepository  // optional, nil = skip linking
}
```

### Step 2: Add linking method
Create a private method on `CommitService`:
```go
func (s *CommitService) linkActionsToCommit(ctx context.Context, commitHash string, files []string) {
    if s.graphRepo == nil {
        return
    }
    // Find unlinked actions that modified any of the committed files
    // Use a reasonable lookback window (e.g., 24 hours)
    since := time.Now().Add(-24 * time.Hour).Unix()
    actions, err := s.graphRepo.UnlinkedActionsForFiles(ctx, files, since)
    if err != nil {
        // Log to verbose, do not propagate
        return
    }
    for _, a := range actions {
        _ = s.graphRepo.CreateActionProduces(ctx, a.ID, commitHash)
    }
}
```

### Step 3: Call linking after successful Commit()
In the commit loop, after `s.git.Commit()` returns successfully, call `s.linkActionsToCommit()` with the commit hash and the group's file list.

### Step 4: Ensure graceful error handling
- Wrap all graph operations in the linking method with error suppression
- Log errors to verbose/debug output only
- Never let a graph error prevent or fail a commit
- If `graphRepo` is nil, skip entirely (zero cost)

### Step 5: Update CommitService constructor
Add an optional `WithGraphRepo` functional option or a setter method. Existing callers that don't use graph features should not need to change.

### Step 6: Run tests
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -count=1 -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -count=1 -v
go vet ./application/...
make build
```

## Success Criteria
- After successful `git.Commit()`, unlinked actions with file overlap are linked via `action_produces`
- `UnlinkedActionsForFiles()` is called with committed file paths and 24h lookback
- Actions with no file overlap are NOT linked
- If `graphRepo` is nil, linking is completely skipped (no error, no panic)
- If any graph operation fails, the error is suppressed and the commit succeeds
- Existing tests continue to pass (nil graphRepo in existing test setup)
- `go vet ./application/...` passes
- `make build` succeeds
