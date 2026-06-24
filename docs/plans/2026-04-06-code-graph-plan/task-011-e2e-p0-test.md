# Task 011: P0 E2E test (RED)

**depends-on**: task-009, task-010

## Description
Write end-to-end tests for the P0 feature set: impact command and commit co-change enhancement. Tests invoke the compiled `git-agent` binary as a subprocess (same pattern as existing e2e tests).

## Execution Context
**Task Number**: 011 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-009 (impact CLI), task-010 (commit enhancement)

## BDD Scenario
```gherkin
Feature: P0 end-to-end tests

  Scenario: impact command returns results for a file
    Given a git repository with 10+ commits and shared file modifications
    And the git-agent binary is built
    When I run git-agent impact <file> in the repository
    Then the exit code is 0
    And stdout contains file paths with co-change percentages

  Scenario: impact --json produces valid JSON
    Given a git repository with indexed history
    When I run git-agent impact <file> --json
    Then stdout is valid JSON
    And JSON contains "target" and "co_changed" keys

  Scenario: Auto-index creates graph.db on first impact run
    Given a git repository with no .git-agent/graph.db
    When I run git-agent impact <file>
    Then .git-agent/graph.db exists after the command
    And exit code is 0

  Scenario: impact for file with no co-changes returns empty
    Given a git repository where "isolated.go" was modified in only 1 commit alone
    When I run git-agent impact isolated.go --json
    Then JSON co_changed array is empty
    And exit code is 0

  Scenario: commit still works with graph feature present
    Given a git repository with graph.db
    And staged changes exist
    When I run git-agent commit (with appropriate test mocking)
    Then the commit succeeds
    And no graph-related error appears in output
```

## Files to Modify/Create
- `e2e/impact_test.go` -- all e2e test functions

## Steps
### Step 1: Review existing e2e patterns
Follow the pattern in existing e2e tests: `TestMain` builds the binary, tests invoke it as a subprocess via `exec.Command`.

### Step 2: Write e2e test functions
- `TestE2E_Impact_ReturnsResults`
- `TestE2E_Impact_JSONOutput`
- `TestE2E_Impact_AutoIndex`
- `TestE2E_Impact_NoCoChanges`
- `TestE2E_Commit_WithGraph`

### Step 3: Set up test repositories
Each test creates a temporary git repository with enough commit history to produce co-change data (at least 3 commits with overlapping file modifications for min_count=3).

### Step 4: Verify tests fail
```bash
go test ./e2e/... -run TestE2E_Impact -v
go test ./e2e/... -run TestE2E_Commit_WithGraph -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./e2e/... -run "TestE2E_Impact|TestE2E_Commit_WithGraph" -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles
- All tests FAIL (Red phase)
- Tests cover: impact results, JSON output, auto-index, empty co-changes, commit with graph
- Tests use subprocess invocation (not direct function calls)
- Each test creates its own isolated git repository
