# Task 006: Incremental Index Impl

**depends-on**: task-006-index-incremental-test

## Description

Extend `GraphService.Index` to support incremental indexing: only process commits after the last indexed hash. Handle idempotent re-runs and respect the `MaxCommits` limit.

## Execution Context

**Task Number**: 006 of 020 (impl)
**Phase**: Core Features (P0)
**Prerequisites**: Failing tests from task-006-index-incremental-test

## BDD Scenario

```gherkin
Scenario: Incremental index after new commits
  Given the repository has an existing graph database
  And the IndexState records commit "abc1234" as last indexed
  And 2 new commits exist after "abc1234"
  When I run "git-agent graph index"
  Then only the 2 new commits should be indexed
  And the IndexState should be updated to the latest commit hash

Scenario: Incremental index is idempotent
  Given the IndexState records the latest commit as last indexed
  When I run "git-agent graph index"
  Then no new data should be added to the graph
  And the command should report "already up to date"
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- enhance Index method with incremental logic

## Steps

### Step 1: Read last indexed commit

At the start of Index, call `repo.GetLastIndexedCommit(ctx)`. If non-empty, pass to `git.CommitLogDetailed(ctx, since, max)`.

### Step 2: Handle idempotent case

If CommitLogDetailed returns zero commits, return IndexResult with `NewCommits: 0` and a message "already up to date".

### Step 3: Pass through MaxCommits

Forward `IndexRequest.MaxCommits` to the git client's max parameter.

### Step 4: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run "TestGraphService_Index_(Incremental|Idempotent|MaxCommits)"` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run "TestGraphService_Index_(Incremental|Idempotent|MaxCommits)" -v
```

## Success Criteria

- Incremental index only processes new commits
- Idempotent runs produce zero new data
- MaxCommits is respected
- All incremental tests pass (Green)
