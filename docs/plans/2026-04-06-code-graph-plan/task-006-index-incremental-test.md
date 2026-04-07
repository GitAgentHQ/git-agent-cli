# Task 006: Incremental Index Test

**depends-on**: task-005

## Description

Write tests for incremental indexing behavior: processing only new commits since the last indexed hash, idempotent re-runs, and the `--max-commits` flag.

## Execution Context

**Task Number**: 006 of 020 (test)
**Phase**: Core Features (P0)
**Prerequisites**: Full index implementation from task-005

## BDD Scenario

```gherkin
Scenario: Incremental index after new commits
  Given the repository has an existing graph database
  And the IndexState records commit "abc1234" as last indexed
  And 2 new commits exist after "abc1234"
  And the new commits modify 3 files
  When I run "git-agent graph index"
  Then only the 2 new commits should be indexed
  And the graph should contain the previously indexed data unchanged
  And the new Commit nodes and MODIFIES edges should be added
  And the IndexState should be updated to the latest commit hash
  And the command should report "indexed 2 new commits"

Scenario: Incremental index is idempotent
  Given the repository has an existing graph database
  And the IndexState records the latest commit as last indexed
  When I run "git-agent graph index"
  Then no new data should be added to the graph
  And the command should report "already up to date"
  And the command should exit with code 0

Scenario: Index limits history depth with --max-commits
  Given the repository has 500 commits
  When I run "git-agent graph index --max-commits 100"
  Then only the most recent 100 commits should be indexed
  And the graph should contain at most 100 Commit nodes
  And the command should exit with code 0
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add incremental test cases)

## Steps

### Step 1: Write incremental index tests

- `TestGraphService_Index_Incremental`: Mock GetLastIndexedCommit to return a known hash. Verify CommitLogDetailed is called with `since=hash`. Verify only new commits are processed.
- `TestGraphService_Index_Idempotent`: Mock GetLastIndexedCommit to return HEAD. Verify no UpsertCommit calls are made. Verify "already up to date" in result.
- `TestGraphService_Index_MaxCommits`: Verify IndexRequest.MaxCommits is passed through to CommitLogDetailed.

### Step 2: Verify tests fail (Red)

- **Verification**: `go test ./application/... -run "TestGraphService_Index_(Incremental|Idempotent|MaxCommits)"` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test ./application/... -run "TestGraphService_Index_(Incremental|Idempotent|MaxCommits)" -v
```

## Success Criteria

- Tests verify incremental behavior (only new commits processed)
- Tests verify idempotent re-runs
- Tests verify max-commits limiting
- All tests FAIL (Red phase)
