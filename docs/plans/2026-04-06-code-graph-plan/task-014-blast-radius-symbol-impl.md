# Task 014: Symbol-Level Blast Radius Impl

**depends-on**: task-014-blast-radius-symbol-test

## Description

Implement symbol-level blast radius queries using CALLS edge traversal in SQLite: find downstream callees, upstream callers, and respect depth limits using recursive SQL CTEs.

## Execution Context

**Task Number**: 014 of 020 (impl)
**Phase**: P1 - Symbol Blast Radius
**Prerequisites**: Failing tests from task-014-blast-radius-symbol-test

## BDD Scenario

```gherkin
Scenario: Query blast radius of a specific function
  When I run "git-agent graph blast-radius --symbol Transform pkg/service.go"
  Then the output should list affected symbols by call chain depth
  And upstream callers should also be listed
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Blast Radius Query)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- extend BlastRadius for symbol mode
- Modify: `infrastructure/graph/sqlite_repository.go` -- add symbol-level SQL queries

## Steps

### Step 1: Implement symbol lookup

When `BlastRadiusRequest.Symbol` is set, find the target symbol by name and file_path. Return error if symbol not found.

### Step 2: Implement downstream callee query (Phase 3 SQL)

```sql
WITH RECURSIVE callees AS (
  SELECT s.id, s.file_path, s.name, s.kind, 1 AS depth
  FROM symbols s
  JOIN calls c ON c.from_symbol_id = (
    SELECT id FROM symbols WHERE name = ? AND file_path = ?
  ) AND c.to_symbol_id = s.id
  UNION ALL
  SELECT s.id, s.file_path, s.name, s.kind, cc.depth + 1
  FROM callees cc
  JOIN calls c ON c.from_symbol_id = cc.id
  JOIN symbols s ON c.to_symbol_id = s.id
  WHERE cc.depth < ?
)
SELECT DISTINCT file_path, name, kind FROM callees;
```

### Step 3: Implement upstream caller query

```sql
WITH RECURSIVE callers AS (
  SELECT s.id, s.file_path, s.name, s.kind, 1 AS depth
  FROM symbols s
  JOIN calls c ON c.to_symbol_id = (
    SELECT id FROM symbols WHERE name = ? AND file_path = ?
  ) AND c.from_symbol_id = s.id
  UNION ALL
  SELECT s.id, s.file_path, s.name, s.kind, cr.depth + 1
  FROM callers cr
  JOIN calls c ON c.to_symbol_id = cr.id
  JOIN symbols s ON c.from_symbol_id = s.id
  WHERE cr.depth < ?
)
SELECT DISTINCT file_path, name, kind FROM callers;
```

### Step 4: Merge results

Combine downstream and upstream results into BlastRadiusResult with `callers` field populated.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run TestGraphService_BlastRadius_Symbol` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run TestGraphService_BlastRadius_Symbol -v
```

## Success Criteria

- Symbol-level blast radius returns callers and callees
- Depth limiting works for call chain traversal
- Results include file paths and symbol metadata
- All symbol blast radius tests pass (Green)
