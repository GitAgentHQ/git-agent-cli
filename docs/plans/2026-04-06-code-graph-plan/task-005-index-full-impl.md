# Task 005: Full Graph Index Impl

**depends-on**: task-005-index-full-test

## Description

Implement `GraphService.Index` for full indexing: walk git history, create Commit/File/Author nodes, MODIFIES/AUTHORED edges, filter binary/vendor files, and update IndexState. This is the core indexing engine.

## Execution Context

**Task Number**: 005 of 020 (impl)
**Phase**: Core Features (P0)
**Prerequisites**: Failing tests from task-005-index-full-test

## BDD Scenario

```gherkin
Scenario: First-time full index of a git repository
  Given the repository has no existing graph database
  And the repository has 3 commits modifying 5 files
  When I run "git-agent graph index"
  Then a graph database should be created at ".git-agent/graph.db"
  And the graph should contain 3 Commit nodes
  And the graph should contain 5 File nodes
  And the graph should contain Author nodes for each unique committer
  And the graph should contain MODIFIES edges linking commits to files
  And the graph should contain AUTHORED edges linking authors to commits
  And the IndexState should record the latest commit hash
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Create: `application/graph_service.go` - Create: `pkg/graph/filter.go` -- file filtering logic (vendor, binary, lock files)

## Steps

### Step 1: Create GraphService struct

Define `GraphService` with `GraphRepository`, `ASTParser`, and `GraphGitClient` dependencies. Constructor function `NewGraphService(...)`.

### Step 2: Implement Index method

Follow the index algorithm from the design:
1. Open DB, InitSchema
2. Read last indexed commit from IndexState (empty = full index)
3. Call `CommitLogDetailed(since, max)` to get commits
4. For each commit: UpsertCommit, UpsertAuthor, CreateAuthored
5. For each modified file: filter, UpsertFile, CreateModifies
6. Update IndexState with latest commit hash
7. Return IndexResult with stats

### Step 3: Implement file filtering

Create `pkg/graph/filter.go` with `ShouldIndex(path string) bool` that excludes vendor directories, node_modules, binary files, and lock files. Reuse patterns from existing `pkg/filter/`.

### Step 4: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run TestGraphService_Index` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run TestGraphService_Index -v

# Existing tests unaffected
make test
```

## Success Criteria

- GraphService.Index creates all expected nodes and edges
- File filtering correctly excludes vendor/binary/lockfiles
- IndexState updated after successful indexing
- All index tests pass (Green)
