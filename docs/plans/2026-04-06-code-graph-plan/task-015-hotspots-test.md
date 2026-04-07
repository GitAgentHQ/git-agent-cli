# Task 015: Hotspots Query Test

**depends-on**: task-005

## Description

Write tests for the hotspots query: change frequency ranking, time-window filtering, JSON output format, limited history handling, and exclusion of test/generated files.

## Execution Context

**Task Number**: 015 of 020 (test)
**Phase**: P1 - Query Commands
**Prerequisites**: Full index (task-005) providing commit/file data

## BDD Scenario

```gherkin
Scenario: Query change frequency hotspots
  When I run "git-agent graph hotspots"
  Then the output should list files ordered by change frequency:
      | file           | changes | last_changed |
      | pkg/service.go | 45      | 2026-03-28   |
      | api/handler.go | 38      | 2026-04-01   |
      | pkg/utils.go   | 12      | 2026-03-15   |
  And the output should highlight the top 10 hotspots by default

Scenario: Query hotspots with time window
  When I run "git-agent graph hotspots --since 2026-03-03"
  Then only changes from the last 30 days should be counted
  And files unchanged in that period should not appear

Scenario: Query change patterns with JSON output
  When I run "git-agent graph hotspots --format json --top 5"
  Then the output should be valid JSON
  And the JSON should contain at most 5 entries
  And each entry should have fields: "path", "changes", "last_changed", "contributors"

Scenario: Query hotspots in repository with limited history
  Given the repository has only 1 commit
  When I run "git-agent graph hotspots"
  Then all files should show a change count of 1

Scenario: Hotspot query excludes generated and test files
  When I run "git-agent graph hotspots --exclude-tests --exclude-generated"
  Then files matching "*_test.go" and "*.test.ts" and "test_*.py" should be excluded
  And files matching "*.generated.go" and "*.pb.go" should be excluded
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Change Pattern Query)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add hotspot test cases)

## Steps

### Step 1: Write hotspot query tests

- `TestGraphService_Hotspots_Ranked`: Verify files ranked by MODIFIES edge count
- `TestGraphService_Hotspots_TimeWindow`: Verify `Since` filter restricts to recent commits
- `TestGraphService_Hotspots_TopN`: Verify `Top` parameter limits results
- `TestGraphService_Hotspots_JSONFormat`: Verify result structure has path, changes, last_changed, contributors
- `TestGraphService_Hotspots_LimitedHistory`: Single commit repo shows all files at count=1
- `TestGraphService_Hotspots_ExcludeTestsAndGenerated`: Verify test/generated file exclusion patterns

### Step 2: Verify tests fail (Red)

- **Verification**: `go test ./application/... -run TestGraphService_Hotspots` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test ./application/... -run TestGraphService_Hotspots -v
```

## Success Criteria

- Tests cover all 5 non-P2 hotspot scenarios
- Time-window filtering tested
- File exclusion patterns tested
- All tests FAIL (Red phase)
