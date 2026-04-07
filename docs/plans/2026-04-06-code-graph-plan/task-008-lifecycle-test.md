# Task 008: Graph Lifecycle Test

**depends-on**: task-003

## Description

Write tests for graph lifecycle management: status reporting, reset/deletion, gitignore integration, error handling (not a git repo, corrupted DB), concurrent access locking, and force re-index.

## Execution Context

**Task Number**: 008 of 020 (test)
**Phase**: Core Features (P0)
**Prerequisites**: SQLite client from task-003

## BDD Scenario

```gherkin
Scenario: Graph status when no index exists
  Given a git repository with no graph database
  When I run "git-agent graph status"
  Then stdout should contain {"exists": false}
  And stdout should contain a "hint" to run graph index
  And the exit code should be 3

Scenario: Graph status when index exists
  Given an indexed repository
  When I run "git-agent graph status"
  Then stdout should contain {"exists": true}
  And stdout should contain node_counts and edge_counts
  And the exit code should be 0

Scenario: Graph reset deletes the database
  Given an indexed repository
  When I run "git-agent graph reset"
  Then ".git-agent/graph.db" should not exist
  And stdout should contain {"deleted": true}

Scenario: Graph index auto-adds graph.db to gitignore
  Given a git repository with no ".git-agent/.gitignore"
  When I run "git-agent graph index"
  Then ".git-agent/.gitignore" should exist
  And ".git-agent/.gitignore" should contain "graph.db"

Scenario: Graph commands outside a git repository return error
  Given the current directory is not a git repository
  When I run "git-agent graph index"
  Then the exit code should be 1
  And stdout should contain {"error": "not a git repository"}

Scenario: Force re-index rebuilds the entire graph
  Given an indexed repository
  When I run "git-agent graph index --force"
  Then the graph database should be rebuilt from scratch
  And all nodes and edges should be recreated
  And the IndexState should record the latest commit hash

Scenario: Graph reset recovers from corrupted database
  Given an indexed repository
  And the graph database files are corrupted
  When I run "git-agent graph blast-radius src/main.go"
  Then the exit code should be 1
  And the error should suggest running "git-agent graph reset"

Scenario: Concurrent indexing is rejected via file lock
  Given an indexed repository
  And another process holds ".git-agent/graph.lock"
  When I run "git-agent graph index"
  Then the exit code should be 1
  And stdout should contain {"error": "graph is being indexed by another process"}
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Lifecycle)

## Files to Modify/Create

- Create: `application/graph_lifecycle_test.go` - Create: `infrastructure/graph/lock_test.go` 
## Steps

### Step 1: Write Status tests

- `TestGraphService_Status_NoIndex`: Returns `{exists: false}` with hint and exit code 3
- `TestGraphService_Status_Indexed`: Returns `{exists: true}` with node/edge counts

### Step 2: Write Reset tests

- `TestGraphService_Reset`: Drops database, returns `{deleted: true}`
- `TestGraphService_Reset_NoDatabase`: Gracefully handles missing database

### Step 3: Write gitignore tests

- `TestGraphService_Index_CreatesGitignore`: After indexing, `.git-agent/.gitignore` contains `graph.db`
- `TestGraphService_Index_ExistingGitignore`: Appends to existing gitignore without duplicating

### Step 4: Write error handling tests

- `TestGraphService_NotGitRepo`: Returns error with "not a git repository"
- `TestGraphService_CorruptedDB`: Returns error suggesting `graph reset`

### Step 5: Write force re-index tests

- `TestGraphService_Index_Force`: With Force=true, drops and rebuilds the database

### Step 6: Write concurrency tests

- `TestGraphLock_AcquireRelease`: File lock acquire/release works
- `TestGraphLock_ConcurrentReject`: Second lock attempt returns error

### Step 7: Verify tests fail (Red)

- **Verification**: `go test ./application/... -run "TestGraphService_(Status|Reset|Index_Creates|NotGitRepo|CorruptedDB|Index_Force)"` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test ./application/... -run "TestGraphService_(Status|Reset)" -v
go test ./infrastructure/graph/... -run TestGraphLock -v
```

## Success Criteria

- Tests cover all 8 lifecycle BDD scenarios
- Lock tests verify file-based concurrency control
- All tests FAIL (Red phase)
