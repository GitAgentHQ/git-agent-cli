# Task 016: Ownership Query Test

**depends-on**: task-005

## Description

Write tests for the ownership query: ranking authors by commit count, recent activity filtering, directory-level aggregation, JSON output format, and single-author edge case.

## Execution Context

**Task Number**: 016 of 020 (test)
**Phase**: P1 - Query Commands
**Prerequisites**: Full index (task-005) providing author/commit data

## BDD Scenario

```gherkin
Scenario: Query who owns a file by commit count
  When I run "git-agent graph ownership pkg/service.go"
  Then the output should list authors ordered by commit count:
      | author        | commits | percentage |
      | alice@dev.com | 15      | 57.7%      |
      | bob@dev.com   | 8       | 30.8%      |
      | carol@dev.com | 3       | 11.5%      |
  And the primary owner should be "alice@dev.com"

Scenario: Query recent maintainers of a module
  Given the following recent commit history in the last 90 days:
      | author        | file           | commits |
      | bob@dev.com   | pkg/service.go | 6       |
      | alice@dev.com | pkg/service.go | 1       |
  When I run "git-agent graph ownership pkg/ --since 90d"
  Then "bob@dev.com" should be ranked first for recent activity

Scenario: Query ownership for a directory at module level
  When I run "git-agent graph ownership pkg/"
  Then the output should aggregate ownership across all files in "pkg/"
  And the output should list the top contributors to the module

Scenario: Query ownership with JSON output
  When I run "git-agent graph ownership pkg/service.go --format json"
  Then the output should be valid JSON
  And each entry should have fields: "email", "name", "commits", "percentage", "last_active"

Scenario: Query ownership for file with single author
  Given "solo.go" has only been modified by "alice@dev.com"
  When I run "git-agent graph ownership solo.go"
  Then the output should show "alice@dev.com" as the sole owner at 100%
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Code Ownership Query)

## Files to Modify/Create

- Modify: `application/graph_service_test.go` (add ownership test cases)

## Steps

### Step 1: Write ownership query tests

- `TestGraphService_Ownership_ByCommitCount`: Verify authors ranked by commit count with percentages
- `TestGraphService_Ownership_RecentActivity`: Verify `Since` filter changes ranking based on recent commits
- `TestGraphService_Ownership_Directory`: Verify directory path aggregates across all files under it
- `TestGraphService_Ownership_JSONFormat`: Verify result structure has email, name, commits, percentage, last_active
- `TestGraphService_Ownership_SingleAuthor`: Verify 100% ownership for sole contributor

### Step 2: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestGraphService_Ownership` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestGraphService_Ownership -v
```

## Success Criteria

- Tests cover all 5 ownership scenarios
- Percentage calculation tested
- Directory aggregation tested
- All tests FAIL (Red phase)
