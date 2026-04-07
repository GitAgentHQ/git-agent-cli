# Task 015: Hotspots Query Impl

**depends-on**: task-015-hotspots-test

## Description

Implement the hotspots query in GraphService and KuzuDB repository: count MODIFIES edges per file, rank by frequency, support time-window filtering and Top-N limiting, exclude test/generated files.

## Execution Context

**Task Number**: 015 of 020 (impl)
**Phase**: P1 - Query Commands
**Prerequisites**: Failing tests from task-015-hotspots-test

## BDD Scenario

```gherkin
Scenario: Query change frequency hotspots
  When I run "git-agent graph hotspots"
  Then the output should list files ordered by change frequency
  And the output should highlight the top 10 hotspots by default
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Change Pattern Query)

## Files to Modify/Create

- Modify: `application/graph_service.go` -- add Hotspots method
- Modify: `infrastructure/graph/kuzu_repository.go` -- implement Hotspots Cypher query

## Steps

### Step 1: Implement Hotspots Cypher query

Count MODIFIES edges per file, optionally filtering by commit timestamp when `Since` is set. Order by count descending. Apply Top-N limit.

### Step 2: Implement file exclusion

When ExcludeTests or ExcludeGenerated flags are set, filter file paths matching test patterns (`*_test.go`, `*.test.ts`, `test_*.py`) and generated patterns (`*.generated.go`, `*.pb.go`).

### Step 3: Implement GraphService.Hotspots

Delegate to repository query. Assemble HotspotsResult with hotspot entries (path, changes, last_changed, contributors) and query timing.

### Step 4: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_Hotspots` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./application/... -run TestGraphService_Hotspots -v
```

## Success Criteria

- Files ranked by change frequency
- Time-window filtering works
- Top-N limiting works
- Test/generated file exclusion works
- All hotspot tests pass (Green)
