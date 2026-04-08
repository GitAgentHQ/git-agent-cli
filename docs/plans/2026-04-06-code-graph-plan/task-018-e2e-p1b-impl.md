# Task 018: P1b E2E implementation

**depends-on**: 018-test

## Description
Make all P1b e2e tests pass. This task involves fixing any integration issues discovered by the e2e tests: wiring problems in CLI commands, missing DB initialization, output format issues, and edge cases in the capture-timeline flow.

## Execution Context
**Task Number**: 018 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: P1b E2E tests (018-test) must exist and FAIL

## BDD Scenario
```gherkin
Feature: P1b E2E Implementation

    Scenario: Full capture-timeline cycle
        Given a git repository with a modified file
        When I run "git-agent capture --source test --tool Edit"
        And I run "git-agent timeline --json"
        Then the timeline JSON should contain a session with source "test"
        And the session should have at least 1 action

    Scenario: All P1b commands coexist with existing commands
        When I run "git-agent --help"
        Then "commit" should appear (existing)
        And "init" should appear (existing)
        And "timeline" should appear (new)
        And "diagnose" should appear (new)
        And "capture" should NOT appear (hidden)

    Scenario: capture creates graph.db on first run
        Given no .git-agent/graph.db exists
        When I run "git-agent capture --source test --tool Edit"
        Then .git-agent/graph.db should be created
        And the schema should be initialized

    Scenario: Existing tests still pass
        When I run "go test ./..."
        Then all existing tests should pass
        And all new P1b tests should pass
```

## Files to Modify/Create
- `cmd/capture.go` -- fix any wiring issues
- `cmd/timeline.go` -- fix any wiring issues
- `cmd/diagnose.go` -- fix any wiring issues
- `application/capture_service.go` -- fix any integration issues
- `application/timeline_service.go` -- fix any integration issues

## Steps
### Step 1: Run e2e tests to identify failures
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./e2e/... -run "TestCapture|TestTimeline|TestDiagnose|TestHelp_Capture" -count=1 -v
```

### Step 2: Fix each failing test
For each test failure:
1. Read the error message
2. Identify the root cause (wiring, DB initialization, output format, etc.)
3. Fix the issue in the appropriate file
4. Re-run the specific test to confirm the fix

### Step 3: Verify no regressions
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...
```

### Step 4: Final build verification
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent capture --source test --tool Edit   # exits 0
./git-agent timeline                            # exits 0
./git-agent diagnose "test"                     # exits 0, stderr: "not yet implemented"
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...
make build
./git-agent capture --source test --tool Edit; echo "exit: $?"
./git-agent timeline; echo "exit: $?"
./git-agent diagnose "test" 2>&1; echo "exit: $?"
```

## Success Criteria
- All P1b e2e tests pass (Green phase)
- All existing tests continue to pass (no regressions)
- `capture` exits 0 in all cases (clean, dirty, error)
- `timeline` shows captured actions after capture
- `diagnose` prints "not yet implemented" to stderr and exits 0
- `capture` is hidden from `--help`
- `timeline` and `diagnose` are visible in `--help`
- `make build` succeeds
- `make test` passes all tests across all packages
