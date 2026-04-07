# Task 008: Graph Lifecycle Impl

**depends-on**: task-008-lifecycle-test

## Description

Implement graph lifecycle operations: Status (node/edge counts), Reset (delete DB), gitignore integration (auto-add graph.db), force re-index (drop + rebuild), error handling (not git repo, corrupted DB), and file-based locking for concurrent access.

## Execution Context

**Task Number**: 008 of 020 (impl)
**Phase**: Core Features (P0)
**Prerequisites**: Failing tests from task-008-lifecycle-test

## BDD Scenario

```gherkin
Scenario: Graph status when no index exists
  Given a git repository with no graph database
  When I run "git-agent graph status"
  Then stdout should contain {"exists": false}
  And the exit code should be 3

Scenario: Graph reset deletes the database
  Given an indexed repository
  When I run "git-agent graph reset"
  Then ".git-agent/graph.db" should not exist

Scenario: Concurrent indexing is rejected via file lock
  Given another process holds ".git-agent/graph.lock"
  When I run "git-agent graph index"
  Then stdout should contain {"error": "graph is being indexed by another process"}
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Lifecycle)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- add Status, Reset methods
- Create: `infrastructure/graph/lock.go` -- file-based lock implementation
- Modify: `application/graph_service.go` -- gitignore integration in Index

## Steps

### Step 1: Implement Status

Call `repo.GetStats(ctx)` to get node/edge counts. If database does not exist, return `GraphStatus{Exists: false}` with hint. Use exit code 3 for missing graph.

### Step 2: Implement Reset

Call `repo.Drop(ctx)` to remove the database file. Return delete confirmation with freed bytes.

### Step 3: Implement gitignore integration

After successful Index, check if `.git-agent/.gitignore` exists. If not, create it with `graph.db`. If it exists but lacks the entry, append it.

### Step 4: Implement file-based lock

Create `infrastructure/graph/lock.go` with `AcquireLock(path)` using `os.OpenFile` with `O_CREATE|O_EXCL`. Release removes the lock file. Index acquires lock at start, releases at end. Return clear error if lock is held.

### Step 5: Implement force re-index

When `IndexRequest.Force` is true, call Reset before proceeding with full index. Clear IndexState so GetLastIndexedCommit returns empty.

### Step 6: Implement error handling

Check if current directory is a git repository before any graph operation. On SQLite corruption errors, wrap with a suggestion to run `graph reset`.

### Step 7: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run "TestGraphService_(Status|Reset)" -v` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run "TestGraphService_(Status|Reset)" -v
go test ./infrastructure/graph/... -run TestGraphLock -v
```

## Success Criteria

- Status reports correct counts or missing graph hint
- Reset deletes database file
- Gitignore auto-updated during indexing
- File lock prevents concurrent indexing
- Force flag triggers full rebuild
- Error handling provides actionable messages
- All lifecycle tests pass (Green)
