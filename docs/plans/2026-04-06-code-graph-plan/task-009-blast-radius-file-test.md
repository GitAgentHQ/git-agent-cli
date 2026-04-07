# Task 009: File-Level Blast Radius Test

**depends-on**: task-005, task-007

## Description

Write tests for file-level blast radius queries: co-change neighbors, import-based dependents, depth limiting, transitive co-changes, empty results, non-existent files, and JSON output format.

## Execution Context

**Task Number**: 009 of 020 (test)
**Phase**: Core Features (P0)
**Prerequisites**: Full index (task-005) and CO_CHANGED edges (task-007)

## BDD Scenario

```gherkin
Scenario: Query blast radius of a single file via co-change and call chain
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should list affected files:
      | file           | reason          | depth |
      | db/store.go    | co-change       | 1     |
      | pkg/utils.go   | co-change       | 1     |
      | api/handler.go | call-dependency | 1     |
  And each result should include the reason for impact
  And co-change results should include coupling strength

Scenario: Query returns empty result for isolated file
  Given the file "config/cfg.go" has no CALLS edges to other files
  And "config/cfg.go" has no CO_CHANGED edges above the threshold
  When I run "git-agent graph blast-radius config/cfg.go"
  Then the output should indicate no blast radius detected
  And the exit code should be 0

Scenario: Agent queries via CLI and gets JSON output
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should be valid JSON
  And the JSON should have "target", "target_type", "co_changed", "importers", "callers" fields
  And each co_changed entry should have "path", "coupling_count", "coupling_strength"
  And the JSON should include a "query_ms" field

Scenario: Blast radius includes transitive co-changes
  Given "a.go" co-changes with "b.go" at strength 0.8
  And "b.go" co-changes with "c.go" at strength 0.6
  When I run "git-agent graph blast-radius a.go --depth 2"
  Then "b.go" should appear at depth 1
  And "c.go" should appear at depth 2
  And deeper transitive co-changes should not appear

Scenario: Query blast radius with depth limit
  When I run "git-agent graph blast-radius --symbol HandleRequest api/handler.go --depth 1"
  Then the output should only include symbols at depth 1
  And symbols beyond depth 1 should not appear in the results

Scenario: Blast radius query on non-existent file
  When I run "git-agent graph blast-radius nonexistent.go"
  Then the command should exit with code 1
  And the error message should indicate the file is not in the graph
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Blast Radius Query)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add blast radius test cases)
- Create: `infrastructure/graph/kuzu_repository_test.go` (blast radius query integration tests)

## Steps

### Step 1: Write unit tests for GraphService.BlastRadius

- `TestGraphService_BlastRadius_CoChange`: Verify co-changed files are returned with coupling data
- `TestGraphService_BlastRadius_Empty`: Verify empty result for isolated file
- `TestGraphService_BlastRadius_NonExistent`: Verify error for file not in graph
- `TestGraphService_BlastRadius_Transitive`: Verify depth-2 transitive co-changes
- `TestGraphService_BlastRadius_DepthLimit`: Verify depth limit is respected

### Step 2: Write integration tests for Cypher queries

- `TestKuzuRepository_BlastRadius`: Seed a real KuzuDB with known data, query blast radius, verify Cypher results match expected output
- `TestKuzuRepository_BlastRadius_JSONFormat`: Verify result structure matches JSON schema (target, target_type, co_changed, importers, callers, query_ms)

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_BlastRadius` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestGraphService_BlastRadius -v
go test -tags graph ./infrastructure/graph/... -run TestKuzuRepository_BlastRadius -v
```

## Success Criteria

- Tests cover all 6 non-symbol blast radius BDD scenarios
- Integration tests verify Cypher query correctness
- JSON output format validated
- All tests FAIL (Red phase)
