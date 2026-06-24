# Task 006: EnsureIndex middleware test (RED)

**depends-on**: task-005

## Description
Write failing tests for the EnsureIndex middleware. This is the auto-indexing logic shared by the `impact` command and the `commit` flow. It decides between full index, full re-index, and incremental index based on database state.

## Execution Context
**Task Number**: 006 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-005 (IndexService working)

## BDD Scenario
```gherkin
Feature: EnsureIndex auto-indexing middleware

  Scenario: No database file triggers full index
    Given no graph.db file exists at the repo path
    When I call EnsureIndex
    Then a full index is performed
    And the result shows IsIncremental=false

  Scenario: Database exists with reachable lastHash triggers incremental
    Given graph.db exists with last_indexed_commit pointing to an ancestor of HEAD
    When I call EnsureIndex
    Then an incremental index is performed (sinceHash=lastHash)
    And the result shows IsIncremental=true
    And only new commits since lastHash are indexed

  Scenario: Database exists with unreachable lastHash triggers full re-index
    Given graph.db exists with last_indexed_commit pointing to a hash not in current history
    When I call EnsureIndex
    Then the database is dropped and recreated
    And a full index is performed
    And the result shows IsIncremental=false

  Scenario: ForceReindex always does full re-index
    Given graph.db exists with valid last_indexed_commit
    When I call EnsureIndex with ForceReindex=true
    Then the database is dropped and recreated
    And a full index is performed

  Scenario: Already up-to-date skips indexing
    Given graph.db exists with last_indexed_commit equal to HEAD
    When I call EnsureIndex
    Then no indexing occurs
    And the result shows CommitsIndexed=0

  Scenario: Progress is reported on stderr
    Given a repository with 100 commits and no graph.db
    When I call EnsureIndex
    Then progress messages are written to the provided writer
```

## Files to Modify/Create
- `application/graph_ensure_index_test.go` -- all test functions

## Steps
### Step 1: Write test functions
- `TestEnsureIndex_NoDB_FullIndex`
- `TestEnsureIndex_ReachableHash_Incremental`
- `TestEnsureIndex_UnreachableHash_FullReindex`
- `TestEnsureIndex_ForceReindex`
- `TestEnsureIndex_AlreadyUpToDate`
- `TestEnsureIndex_Progress`

### Step 2: Use mocks or real implementations
Tests should use mock GraphRepository and mock GraphGitClient to control scenarios (reachable/unreachable hashes, existing DB state).

### Step 3: Verify tests fail
```bash
go test ./application/... -run TestEnsureIndex -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestEnsureIndex -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles
- All tests FAIL (Red phase)
- Tests cover: no DB, reachable hash, unreachable hash, force reindex, already up-to-date, progress
- Tests use dependency injection via domain interfaces
