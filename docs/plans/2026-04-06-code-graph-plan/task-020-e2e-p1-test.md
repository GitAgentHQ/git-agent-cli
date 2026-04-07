# Task 020: P1 E2E Tests

**depends-on**: task-019

## Description

Write end-to-end tests for P1 graph features: capture, timeline, hotspots, and ownership. Tests follow the existing e2e pattern with subprocess invocation against a temporary git repository.

## Execution Context

**Task Number**: 020 of 020 (test)
**Phase**: Integration (P1)
**Prerequisites**: P1 CLI wiring from task-019

## BDD Scenario

```gherkin
Scenario: Capture an agent edit action (E2E)
  Given a git repository with indexed graph
  And I modify a file in the working directory
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then the output should contain a valid action_id

Scenario: Timeline shows captured actions (E2E)
  Given captured actions exist in the graph
  When I run "git-agent graph timeline --since 1h"
  Then the output should contain sessions with actions

Scenario: Hotspots ranks files correctly (E2E)
  Given a repository with multiple commits per file
  When I run "git-agent graph hotspots --top 5"
  Then files should be ranked by change count

Scenario: Ownership shows correct author (E2E)
  Given a repository where "alice@dev.com" authored most commits to "main.go"
  When I run "git-agent graph ownership main.go"
  Then "alice@dev.com" should appear as the primary owner

Scenario: Index with AST extracts symbols (E2E)
  Given a repository with Go source files
  When I run "git-agent graph index --ast"
  And I run "git-agent graph status"
  Then the status should show symbol count > 0
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (all P1 features)

## Files to Modify/Create

- Modify: `e2e/graph_test.go` (add P1 E2E tests)

## Steps

### Step 1: Write capture E2E tests

- `TestE2E_GraphCapture`: Modify a file, run capture, verify JSON output with action_id
- `TestE2E_GraphCapture_NoDiff`: Run capture without changes, verify skipped output

### Step 2: Write timeline E2E tests

- `TestE2E_GraphTimeline`: After captures, run timeline, verify sessions and actions in output
- `TestE2E_GraphTimeline_FilterBySource`: Verify source filter works end-to-end

### Step 3: Write hotspots E2E test

- `TestE2E_GraphHotspots`: After indexing, query hotspots, verify ranked output

### Step 4: Write ownership E2E test

- `TestE2E_GraphOwnership`: After indexing, query ownership, verify author ranking

### Step 5: Write AST index E2E test

- `TestE2E_GraphIndex_AST`: Index with --ast, verify status shows symbol count > 0

### Step 6: Verify and fix integration issues

Run all E2E tests. Fix any integration issues discovered (data flow, serialization, path handling).

- **Verification**: `go test ./e2e/... -run TestE2E_Graph` -- all tests PASS

## Verification Commands

```bash
# All graph E2E tests pass
go test ./e2e/... -run TestE2E_Graph -v

# All unit tests pass
go test ./application/... ./infrastructure/graph/... ./infrastructure/treesitter/... -v

# Existing tests unaffected
make test
```

## Success Criteria

- E2E tests cover capture, timeline, hotspots, ownership, and AST indexing
- All E2E tests pass
- No regression in existing tests
- Full P0 + P1 pipeline works end-to-end
