# Task 014: Symbol-Level Blast Radius Test

**depends-on**: task-009, task-013

## Description

Write tests for symbol-level blast radius queries: given a function/class name, find callers and callees across the call graph, respect depth limits, and return both upstream callers and downstream dependencies.

## Execution Context

**Task Number**: 014 of 020 (test)
**Phase**: P1 - Symbol Blast Radius
**Prerequisites**: File-level blast radius (task-009) and AST indexing (task-013)

## BDD Scenario

```gherkin
Scenario: Query blast radius of a specific function
  When I run "git-agent graph blast-radius --symbol Transform pkg/service.go"
  Then the output should list affected symbols by call chain depth:
      | symbol   | file         | depth |
      | Format   | pkg/utils.go | 1     |
      | Sanitize | pkg/utils.go | 1     |
  And upstream callers should also be listed:
      | symbol        | file           | depth |
      | Process       | pkg/service.go | 1     |
      | HandleRequest | api/handler.go | 2     |

Scenario: Query blast radius with depth limit
  When I run "git-agent graph blast-radius --symbol HandleRequest api/handler.go --depth 1"
  Then the output should only include symbols at depth 1:
      | symbol   | file           |
      | Process  | pkg/service.go |
      | Validate | api/handler.go |
  And symbols beyond depth 1 should not appear in the results
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Blast Radius Query)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add symbol blast radius tests)
- Modify: `infrastructure/graph/kuzu_repository_test.go` (add symbol query integration tests)

## Steps

### Step 1: Write symbol blast radius unit tests

- `TestGraphService_BlastRadius_Symbol`: Given a symbol name and file, verify callers and callees are returned
- `TestGraphService_BlastRadius_Symbol_DepthLimit`: Verify depth limit clips the call chain
- `TestGraphService_BlastRadius_Symbol_UpstreamCallers`: Verify reverse call chain traversal

### Step 2: Write Cypher query integration tests

- `TestKuzuRepository_BlastRadius_Symbol`: Seed graph with known call relationships, verify Cypher query returns correct results at each depth

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_BlastRadius_Symbol` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestGraphService_BlastRadius_Symbol -v
```

## Success Criteria

- Tests verify symbol-level call chain traversal
- Tests verify depth limiting
- Tests verify both upstream callers and downstream callees
- All tests FAIL (Red phase)
