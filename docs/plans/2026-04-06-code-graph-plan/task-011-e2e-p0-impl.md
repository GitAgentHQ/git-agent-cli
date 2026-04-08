# Task 011: P0 E2E implementation (GREEN)

**depends-on**: task-011-test

## Description
Make the P0 E2E tests pass. If all prior tasks (001-010) are implemented correctly, these tests should already pass. This task is a verification checkpoint.

## Execution Context
**Task Number**: 011 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-011-test (failing E2E tests exist), all of tasks 001-010

## BDD Scenario
```gherkin
Feature: P0 E2E verification

  Scenario: All P0 E2E tests pass
    Given tasks 001 through 010 are complete
    When I run the P0 E2E tests
    Then all tests pass (Green phase)

  Scenario: Full test suite still passes
    Given P0 features are complete
    When I run make test
    Then all tests pass including existing tests
```

## Files to Modify/Create
- No new files expected -- fix issues in prior task implementations if tests fail

## Steps
### Step 1: Build the binary
```bash
make build
```

### Step 2: Run E2E tests
```bash
go test ./e2e/... -run "TestE2E_Impact|TestE2E_Commit_WithGraph" -v
```

### Step 3: Fix any failures
If tests fail, trace the issue back to the responsible task (001-010) and fix there.

### Step 4: Run full test suite
```bash
make test
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
go test ./e2e/... -run "TestE2E_Impact|TestE2E_Commit_WithGraph" -v
# All tests must PASS
make test
# Full suite must PASS
```

## Success Criteria
- All `TestE2E_Impact_*` and `TestE2E_Commit_WithGraph` tests pass
- `make test` passes (full suite including existing tests)
- `make build` produces a working binary
- `git-agent impact --help` works
- P0 milestone complete: impact command + invisible commit enhancement
