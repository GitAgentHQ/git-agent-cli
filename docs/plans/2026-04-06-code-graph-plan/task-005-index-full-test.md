# Task 005: Full Graph Index Test

**depends-on**: task-003, task-004

## Description

Write tests for the full graph indexing flow in `GraphService.Index`: parsing git history, creating nodes and edges, filtering binary/vendor files, and handling large repositories. Tests mock GraphRepository and GraphGitClient.

## Execution Context

**Task Number**: 005 of 020 (test)
**Phase**: Core Features (P0)
**Prerequisites**: KuzuDB client (task-003) and git graph client (task-004)

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
  And the command should exit with code 0

Scenario: Index skips binary and vendor files
  Given the repository contains files:
      | path                   | type     |
      | src/main.go            | source   |
      | vendor/lib/dep.go      | vendor   |
      | assets/logo.png        | binary   |
      | node_modules/pkg/x.js  | vendor   |
      | go.sum                 | lockfile |
  When I run "git-agent graph index"
  Then the graph should contain a File node for "src/main.go"
  And the graph should not contain File nodes for vendor directories
  And the graph should not contain File nodes for binary files
  And the graph should not contain File nodes for lock files

Scenario: Index handles large repositories gracefully
  Given the repository has 10000 commits modifying 5000 files
  When I run "git-agent graph index"
  Then the indexing should complete without running out of memory
  And bulk import should be used for the initial load
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Create: `application/graph_service_test.go` (with `//go:build graph` tag)

## Steps

### Step 1: Create test file with mocks

Create `application/graph_service_test.go` with mock implementations of GraphRepository, ASTParser, and GraphGitClient.

### Step 2: Write full index tests

- `TestGraphService_Index_FirstTime`: Verifies commit, file, author nodes and MODIFIES/AUTHORED edges are created for a fresh repository
- `TestGraphService_Index_SkipsBinaryAndVendor`: Verifies vendor/, node_modules/, binary files (.png), and lock files (go.sum) are excluded
- `TestGraphService_Index_SetsIndexState`: Verifies IndexState is updated with the latest commit hash after indexing
- `TestGraphService_Index_EmptyRepo`: Verifies graceful handling of repository with no commits

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_Index` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestGraphService_Index -v
```

## Success Criteria

- Test file created with mocks for all dependencies
- Tests cover full index flow with node/edge creation
- Tests verify file filtering logic
- All tests FAIL (Red phase)
