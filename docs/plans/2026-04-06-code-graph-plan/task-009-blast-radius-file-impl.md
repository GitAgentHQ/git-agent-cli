# Task 009: File-Level Blast Radius Impl

**depends-on**: task-009-blast-radius-file-test

## Description

Implement file-level blast radius queries in GraphService and SQLite repository: CO_CHANGED neighbor lookup, IMPORTS reverse lookup, depth-limited traversal, transitive co-changes, and structured JSON output.

## Execution Context

**Task Number**: 009 of 020 (impl)
**Phase**: Core Features (P0)
**Prerequisites**: Failing tests from task-009-blast-radius-file-test

## BDD Scenario

```gherkin
Scenario: Query blast radius of a single file via co-change and call chain
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should list affected files with reason and depth

Scenario: Agent queries via CLI and gets JSON output
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should be valid JSON
  And the JSON should have "target", "target_type", "co_changed", "importers", "callers" fields
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Blast Radius Query)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- add BlastRadius method
- Modify: `infrastructure/graph/sqlite_repository.go` -- implement BlastRadius SQL queries

## Steps

### Step 1: Implement BlastRadius in SQLite repository

Implement the two-phase parameterized SQL query from the design:
- Phase 1: CO_CHANGED neighbors with `WHERE cc.coupling_count >= $minCount`
- Phase 2: IMPORTS reverse lookup `(importer:File)-[:IMPORTS]->(target:File)`
- Merge and deduplicate results, preserving reason and coupling data

### Step 2: Implement depth-limited traversal

For transitive co-changes (depth > 1), use recursive SQL or iterative expansion up to `BlastRadiusRequest.Depth`.

### Step 3: Implement GraphService.BlastRadius

Validate target file exists in graph. Delegate to repository query. Assemble BlastRadiusResult with target, target_type, co_changed, importers, callers arrays, and query_ms timing.

### Step 4: Handle edge cases

Non-existent file returns error with exit code 1. Isolated file returns empty co_changed/importers/callers arrays with exit code 0.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run TestGraphService_BlastRadius` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run TestGraphService_BlastRadius -v
go test ./infrastructure/graph/... -run TestSQLiteRepository_BlastRadius -v
```

## Success Criteria

- Co-change neighbors returned with coupling data
- Transitive co-changes respect depth limit
- IMPORTS reverse lookup works
- JSON output matches schema
- Error handling for non-existent and isolated files
- All blast radius tests pass (Green)
