# Task 005: Full graph indexing service implementation (GREEN)

**depends-on**: task-005-test

## Description
Implement the full graph indexing service that walks git log output and batch-inserts into SQLite. Uses transaction wrapping for performance.

## Execution Context
**Task Number**: 005 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-005-test (failing tests exist)

## BDD Scenario
```gherkin
Feature: Full graph indexing implementation

  Scenario: All index tests pass
    Given the IndexService is implemented
    When I run the index tests
    Then all tests pass (Green phase)
```

## Files to Modify/Create
- `application/graph_index_service.go` -- IndexService struct and methods

## Steps
### Step 1: Define IndexService struct
```go
type IndexService struct {
    repo graph.GraphRepository
    git  graph.GraphGitClient
}
```

### Step 2: Implement FullIndex method
1. Call `git.CommitLogDetailed(ctx, "", 0)` to get all commits
2. Begin a transaction via the repository
3. For each commit:
   - InsertCommit (hash, message, timestamp)
   - InsertAuthor (email, name) -- upsert
   - Link commit to author via authored table
   - For each file change:
     - InsertFileChange (commit_hash, file_path, status, old_path, additions, deletions)
     - Update file total_commits count
     - If status == "R": InsertRename (old_path, new_path, commit_hash)
4. Store `last_indexed_commit` = HEAD in index_state
5. Commit transaction

### Step 3: Implement helper methods
- `updateFileCounts` -- aggregate commit counts per file path
- `storeIndexState` -- key-value write to index_state table

### Step 4: Run tests
```bash
go test ./application/... -run TestIndexService -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestIndexService -v
# All tests must PASS
make build
make test
```

## Success Criteria
- All `TestIndexService_*` tests pass
- Commits, files, authors, modifies, authored, renames tables populated correctly
- index_state stores last_indexed_commit
- File commit counts are accurate
- `make build` and `make test` pass
