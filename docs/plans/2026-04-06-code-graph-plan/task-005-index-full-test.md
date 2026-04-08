# Task 005: Full graph indexing service test (RED)

**depends-on**: task-003, task-004

## Description
Write failing tests for the full graph indexing service. This service walks `git log`, batch-inserts commit/file/author data into SQLite, handles renames (R status -> renames table), and stores the last indexed commit in index_state.

## Execution Context
**Task Number**: 005 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-003 (SQLite client working), task-004 (git graph client working)

## BDD Scenario
```gherkin
Feature: Full graph indexing

  Scenario: Index empty repository
    Given a git repository with no commits
    When I run full index
    Then IndexResult.CommitsIndexed is 0
    And no error occurs

  Scenario: Index repository with commits
    Given a git repository with 5 commits modifying 3 files
    When I run full index
    Then IndexResult.CommitsIndexed is 5
    And the commits table has 5 rows
    And the modifies table reflects all file changes
    And the authors table has entries for each unique author
    And the authored table links commits to authors

  Scenario: Index handles renames
    Given a git repository where "old.go" was renamed to "new.go"
    When I run full index
    Then the renames table has an entry with old_path="old.go", new_path="new.go"
    And the modifies entry has status "R"

  Scenario: Index stores last indexed commit
    Given a git repository with commits
    When I run full index
    Then index_state contains key "last_indexed_commit" with value equal to HEAD

  Scenario: Index updates file commit counts
    Given a git repository where "main.go" appears in 3 commits
    When I run full index
    Then files table has total_commits=3 for path "main.go"

  Scenario: Batch insert performance
    Given a git repository with 100 commits
    When I run full index
    Then all commits are inserted within a single transaction
    And IndexResult.CommitsIndexed is 100
```

## Files to Modify/Create
- `application/graph_index_test.go` -- all test functions

## Steps
### Step 1: Write test helpers
Create helpers for setting up temp repos with various commit histories. Use the real SQLiteClient and either real or mock GraphGitClient depending on test needs.

### Step 2: Write test functions
- `TestIndexService_EmptyRepo`
- `TestIndexService_FullIndex`
- `TestIndexService_Renames`
- `TestIndexService_LastIndexedCommit`
- `TestIndexService_FileCommitCounts`
- `TestIndexService_BatchInsert`

### Step 3: Verify tests fail
```bash
go test ./application/... -run TestIndexService -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestIndexService -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles
- All tests FAIL (Red phase)
- Tests cover: empty repo, full index, renames, last indexed commit, file counts, batch insert
- Tests use domain interfaces (GraphRepository, GraphGitClient) for dependency injection
