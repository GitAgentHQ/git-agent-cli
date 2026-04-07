# Task 007: CO_CHANGED Computation Impl

**depends-on**: task-007-co-change-test

## Description

Implement CO_CHANGED edge computation in the KuzuDB repository layer. After all commits are indexed, recompute co-change relationships by querying commit-file pairings, calculating coupling count and strength, and storing edges that meet the minimum threshold.

## Execution Context

**Task Number**: 007 of 020 (impl)
**Phase**: Core Features (P0)
**Prerequisites**: Failing tests from task-007-co-change-test

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

- Create: `infrastructure/graph/co_change.go` (with `//go:build graph` tag)
- Modify: `infrastructure/graph/kuzu_repository.go` -- implement RecomputeCoChanged

## Steps

### Step 1: Implement co-change computation

Query all commit-file pairs from MODIFIES edges. Group by file pairs to count co-occurrences. Compute coupling_strength = co_count / max(total_commits_a, total_commits_b).

### Step 2: Implement threshold filtering

Only create CO_CHANGED edges where coupling_count >= minCount (default 3). Skip commits with more than `maxFilesPerCommit` modified files (default 50).

### Step 3: Implement RecomputeCoChanged in repository

Delete existing CO_CHANGED edges, then insert new ones from the computation. Use MERGE to handle idempotent recomputation.

### Step 4: Wire into GraphService.Index

Call `repo.RecomputeCoChanged(ctx, minCount)` after all commits are indexed.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./infrastructure/graph/... -run TestCoChange` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./infrastructure/graph/... -run TestCoChange -v
```

## Success Criteria

- CO_CHANGED edges correctly computed from commit-file data
- Coupling strength calculated accurately
- Threshold filtering works
- Large commit exclusion works
- All co-change tests pass (Green)
