# Task 007: CO_CHANGED Computation Test

**depends-on**: task-005

## Description

Write tests for CO_CHANGED edge computation: detecting files that frequently change together in the same commits, calculating coupling strength, and respecting the minimum co-change threshold.

## Execution Context

**Task Number**: 007 of 020 (test)
**Phase**: Core Features (P0)
**Prerequisites**: Full index implementation from task-005

## BDD Scenario

```gherkin
Scenario: Index computes CO_CHANGED edges
  Given the repository has commits where "a.go" and "b.go" are modified together 5 times
  And "a.go" has been modified 8 times total
  And "b.go" has been modified 6 times total
  When I run "git-agent graph index"
  Then the graph should contain a CO_CHANGED edge from "a.go" to "b.go"
  And the edge should have coupling_count of 5
  And the edge should have coupling_strength of approximately 0.625
  And pairs with fewer than 3 co-changes should not have CO_CHANGED edges
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Create: `infrastructure/graph/co_change_test.go` (with `//go:build graph` tag)

## Steps

### Step 1: Write co-change computation tests

- `TestCoChange_ComputeEdges`: Given known commit-file pairings, verify CO_CHANGED edges are created with correct coupling_count
- `TestCoChange_CouplingStrength`: Verify strength = co_occurrences / max(individual_commits_a, individual_commits_b). For (5, 8, 6) -> 5/8 = 0.625
- `TestCoChange_MinimumThreshold`: Pairs with fewer than 3 co-changes should produce no CO_CHANGED edge
- `TestCoChange_SkipsLargeCommits`: Commits touching more than 50 files should be excluded from co-change computation

### Step 2: Write integration test

- `TestCoChange_Integration`: Using a real KuzuDB instance with seeded data, verify RecomputeCoChanged produces correct edges

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./infrastructure/graph/... -run TestCoChange` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./infrastructure/graph/... -run TestCoChange -v
```

## Success Criteria

- Tests cover co-change edge creation, strength calculation, and threshold filtering
- Tests verify large commit exclusion
- Integration test uses real KuzuDB
- All tests FAIL (Red phase)
