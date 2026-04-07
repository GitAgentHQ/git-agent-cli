# Task 020: P1 E2E Impl

**depends-on**: task-020-e2e-p1-test

## Description

Fix integration issues discovered during P1 E2E testing. Ensure the full P0 + P1 pipeline works end-to-end: index with AST, capture actions, query timeline, hotspots, and ownership.

## Execution Context

**Task Number**: 020 of 020 (impl)
**Phase**: Integration (P1)
**Prerequisites**: Failing E2E tests from task-020-e2e-p1-test

## BDD Scenario

```gherkin
Scenario: Full pipeline works end-to-end
  Given a git repository with commits and source files
  When I run "git-agent graph index --ast"
  And I modify a file and run "git-agent graph capture --source claude-code --tool Edit"
  And I run "git-agent graph timeline --since 1h"
  And I run "git-agent graph hotspots --top 5"
  And I run "git-agent graph ownership main.go"
  Then all commands should produce valid JSON output
  And all commands should exit with code 0
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md`

## Files to Modify/Create

- May modify any files to fix integration issues discovered by E2E tests
- Focus areas: `cmd/graph_*.go`, `application/graph_*.go`, `infrastructure/graph/kuzu_repository.go`

## Steps

### Step 1: Run all E2E tests and diagnose failures

Run all graph E2E tests, read error output, and identify integration issues.

### Step 2: Fix integration issues

Address each failing E2E test. Common issues: Cypher query syntax, session ID generation, diff parsing, flag propagation, LLM client wiring.

### Step 3: Verify all tests pass (Green)

- **Verification**: `go test -tags graph ./e2e/... -run TestE2E_Graph` -- all tests PASS
- **Verification**: `go test -tags graph ./...` -- all graph tests pass
- **Verification**: `make test` -- existing tests unaffected

## Verification Commands

```bash
# All graph tests pass
go test -tags graph ./e2e/... -run TestE2E_Graph -v
go test -tags graph ./... -v

# Existing tests unaffected
make test
```

## Success Criteria

- All P0 + P1 E2E tests pass (Green)
- No regression in existing tests
- Full pipeline works: index -> capture -> timeline -> hotspots -> ownership -> blast-radius
