# Task 014: Symbol-Level Blast Radius Impl

**depends-on**: task-014-blast-radius-symbol-test

## Description

Implement symbol-level blast radius queries using CALLS edge traversal in KuzuDB: find downstream callees, upstream callers, and respect depth limits using Cypher path queries.

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
- Modify: `infrastructure/graph/kuzu_repository.go` -- add symbol-level Cypher queries

## Steps

### Step 1: Implement symbol lookup

When `BlastRadiusRequest.Symbol` is set, find the target symbol by name and file_path. Return error if symbol not found.

### Step 2: Implement downstream callee query (Phase 3 Cypher)

```cypher
MATCH (target:Symbol)
WHERE target.name = $symbolName AND target.file_path = $filePath
MATCH (target)-[:CALLS*1..$depth]->(callee:Symbol)
RETURN DISTINCT callee.file_path, callee.name, callee.kind
```

### Step 3: Implement upstream caller query

```cypher
MATCH (target:Symbol)
WHERE target.name = $symbolName AND target.file_path = $filePath
MATCH (caller:Symbol)-[:CALLS*1..$depth]->(target)
RETURN DISTINCT caller.file_path, caller.name, caller.kind
```

### Step 4: Merge results

Combine downstream and upstream results into BlastRadiusResult with `callers` field populated.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_BlastRadius_Symbol` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./application/... -run TestGraphService_BlastRadius_Symbol -v
```

## Success Criteria

- Symbol-level blast radius returns callers and callees
- Depth limiting works for call chain traversal
- Results include file paths and symbol metadata
- All symbol blast radius tests pass (Green)
