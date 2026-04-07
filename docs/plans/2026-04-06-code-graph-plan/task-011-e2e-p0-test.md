# Task 011: P0 E2E Tests

**depends-on**: task-010

## Description

Write end-to-end tests for P0 graph features following the existing e2e pattern: TestMain builds the binary once (with graph tag), then each test invokes it as a subprocess against a real temporary git repository.

## Execution Context

**Task Number**: 011 of 020 (test)
**Phase**: Integration (P0)
**Prerequisites**: CLI wiring from task-010

## BDD Scenario

```gherkin
Scenario: First-time full index of a git repository
  Given a git repository at a temporary test directory
  And the repository has 3 commits modifying 5 files
  When I run "git-agent graph index"
  Then a graph database should be created at ".git-agent/graph.db"
  And the command should exit with code 0

Scenario: Agent queries via CLI and gets JSON output
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should be valid JSON

Scenario: Graph status when no index exists
  Given a git repository with no graph database
  When I run "git-agent graph status"
  Then stdout should contain {"exists": false}
  And the exit code should be 3

Scenario: Graph reset deletes the database
  Given an indexed repository
  When I run "git-agent graph reset"
  Then ".git-agent/graph.db" should not exist
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing, Blast Radius, Graph Lifecycle)

## Files to Modify/Create

- Create: `e2e/graph_test.go` (with `//go:build graph` tag)

## Steps

### Step 1: Set up test infrastructure

In `TestMain`, build the binary with `-tags graph`. Create a temporary git repository with multiple commits, authors, and file modifications (same pattern as existing e2e tests).

### Step 2: Write graph index E2E test

- `TestE2E_GraphIndex`: Run `git-agent graph index`, verify JSON output contains indexed_commits > 0, exit code 0
- `TestE2E_GraphIndex_Incremental`: Index, add commit, re-index, verify new_commits = 1

### Step 3: Write blast-radius E2E test

- `TestE2E_GraphBlastRadius`: Index, then query blast radius of a file with known co-changes, verify JSON output
- `TestE2E_GraphBlastRadius_NonExistent`: Query non-existent file, verify exit code 1

### Step 4: Write status E2E tests

- `TestE2E_GraphStatus_NoIndex`: Without indexing, verify exit code 3 and exists=false
- `TestE2E_GraphStatus_Indexed`: After indexing, verify exit code 0 and node counts

### Step 5: Write reset E2E test

- `TestE2E_GraphReset`: Index, reset, verify graph.db directory is removed

### Step 6: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./e2e/... -run TestE2E_Graph` -- tests MUST FAIL (need binary + test data)

## Verification Commands

```bash
# Tests should fail initially, then pass after impl
go test -tags graph ./e2e/... -run TestE2E_Graph -v
```

## Success Criteria

- E2E tests follow existing pattern (subprocess invocation)
- Tests use `//go:build graph` tag
- All P0 scenarios covered end-to-end
- All tests FAIL (Red phase)
