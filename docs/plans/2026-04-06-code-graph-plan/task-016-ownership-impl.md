# Task 016: Ownership Query Impl

**depends-on**: task-016-ownership-test

## Description

Implement the ownership query in GraphService and KuzuDB repository: count commits per author for a file or directory, rank by count, calculate percentages, support time-window filtering.

## Execution Context

**Task Number**: 016 of 020 (impl)
**Phase**: P1 - Query Commands
**Prerequisites**: Failing tests from task-016-ownership-test

## BDD Scenario

```gherkin
Scenario: Query who owns a file by commit count
  When I run "git-agent graph ownership pkg/service.go"
  Then the output should list authors ordered by commit count
  And the primary owner should be "alice@dev.com"
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Code Ownership Query)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- add Ownership method
- Modify: `infrastructure/graph/kuzu_repository.go` -- implement Ownership Cypher query

## Steps

### Step 1: Implement Ownership Cypher query

Join Author -> AUTHORED -> Commit -> MODIFIES -> File. Group by author, count commits, calculate percentage. Optionally filter by commit timestamp for `Since`.

### Step 2: Handle directory paths

When target path ends with `/`, match all files with that prefix. Aggregate commit counts across all matching files.

### Step 3: Implement GraphService.Ownership

Delegate to repository query. Assemble OwnershipResult with author entries (email, name, commits, percentage, last_active) and total_commits.

### Step 4: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_Ownership` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./application/... -run TestGraphService_Ownership -v
```

## Success Criteria

- Authors ranked by commit count with correct percentages
- Time-window filtering works
- Directory-level aggregation works
- All ownership tests pass (Green)
