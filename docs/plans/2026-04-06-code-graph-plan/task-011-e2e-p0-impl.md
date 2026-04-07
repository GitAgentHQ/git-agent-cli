# Task 011: P0 E2E Impl

**depends-on**: task-011-e2e-p0-test

## Description

Fix any issues discovered during E2E testing, ensure the full P0 pipeline works end-to-end: index a real repository, query blast radius, check status, and reset. This task focuses on integration fixes, not new features.

## Execution Context

**Task Number**: 011 of 020 (impl)
**Phase**: Integration (P0)
**Prerequisites**: Failing E2E tests from task-011-e2e-p0-test

## BDD Scenario

```gherkin
Scenario: First-time full index of a git repository
  Given a git repository at a temporary test directory
  And the repository has 3 commits modifying 5 files
  When I run "git-agent graph index"
  Then a graph database should be created at ".git-agent/graph.db"
  And the graph should contain 3 Commit nodes
  And the command should exit with code 0
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- May modify any P0 files to fix integration issues discovered by E2E tests
- Focus areas: `cmd/graph_*.go`, `application/graph_service.go`, `infrastructure/graph/kuzu_repository.go`

## Steps

### Step 1: Run E2E tests and diagnose failures

Run the E2E tests, read error output, and identify integration issues (data flow, serialization, path handling, etc.).

### Step 2: Fix integration issues

Address each failing E2E test. Common issues: JSON marshaling, path resolution, KuzuDB connection lifecycle in subprocess, exit code propagation.

### Step 3: Verify all P0 tests pass (Green)

- **Verification**: `go test -tags graph ./e2e/... -run TestE2E_Graph` -- all tests PASS
- **Verification**: `go test -tags graph ./application/... ./infrastructure/graph/...` -- unit tests still pass
- **Verification**: `make test` -- existing tests unaffected

## Verification Commands

```bash
# E2E tests pass
go test -tags graph ./e2e/... -run TestE2E_Graph -v

# Unit tests still pass
go test -tags graph ./application/... ./infrastructure/graph/... -v

# Existing tests unaffected
make test
```

## Success Criteria

- All P0 E2E tests pass (Green)
- No regression in existing tests
- Full P0 pipeline works: index -> query -> status -> reset
