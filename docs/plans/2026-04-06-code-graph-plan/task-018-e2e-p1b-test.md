# Task 018: P1b E2E test

**depends-on**: 013, 015, 016

## Description
Write end-to-end tests for the P1b action tracking pipeline. Tests build the `git-agent` binary and invoke it as a subprocess (following the existing e2e pattern in `e2e/`). Tests cover `capture`, `timeline`, and `diagnose` commands. All tests must FAIL initially (Red phase).

## Execution Context
**Task Number**: 018 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: capture CLI (013), timeline CLI (015), and diagnose stub (016) must be complete

## BDD Scenario
```gherkin
Feature: P1b E2E Tests
    As a developer
    I want end-to-end tests for the action tracking pipeline
    So that I can verify capture, timeline, and diagnose work as a complete system

    Background:
        Given the git-agent binary is built via TestMain
        And a temporary git repository with at least 1 commit

    Scenario: capture exits 0 with no changes
        Given the working directory is clean (no uncommitted changes)
        When I run "git-agent capture --source test --tool Edit"
        Then the exit code should be 0
        And stdout should be empty

    Scenario: capture exits 0 with changes
        Given I create a new file "test.go" with content
        When I run "git-agent capture --source test --tool Edit"
        Then the exit code should be 0
        And .git-agent/graph.db should exist

    Scenario: capture exits 0 even on error
        Given an invalid repository path
        When I run "git-agent capture --source test --tool Edit"
        Then the exit code should be 0

    Scenario: timeline shows captured actions
        Given I captured an action by modifying "test.go" and running capture
        When I run "git-agent timeline --json"
        Then the exit code should be 0
        And the JSON output should contain at least 1 session
        And the session should have source "test"

    Scenario: timeline exits 0 with empty graph
        Given no capture has been performed
        When I run "git-agent timeline"
        Then the exit code should be 0

    Scenario: diagnose prints stub message
        When I run "git-agent diagnose 'test bug'"
        Then the exit code should be 0
        And stderr should contain "not yet implemented"
        And stdout should be empty

    Scenario: capture is hidden from help
        When I run "git-agent --help"
        Then "capture" should NOT appear in the output
        And "timeline" should appear in the output
        And "diagnose" should appear in the output
```

## Files to Modify/Create
- `e2e/capture_timeline_test.go` -- new e2e test file

## Steps
### Step 1: Create e2e/capture_timeline_test.go
Follow the existing e2e test pattern:
- Use `TestMain` (already builds the binary in existing e2e tests)
- Use helper functions from `e2e/helpers_test.go` for running the binary
- Create a temp git repository with an initial commit for each test

### Step 2: Implement test functions
```go
func TestCapture_ExitsZero_NoChanges(t *testing.T)
func TestCapture_ExitsZero_WithChanges(t *testing.T)
func TestCapture_ExitsZero_OnError(t *testing.T)
func TestTimeline_ShowsCapturedActions(t *testing.T)
func TestTimeline_ExitsZero_EmptyGraph(t *testing.T)
func TestDiagnose_PrintsStubMessage(t *testing.T)
func TestHelp_CaptureHidden_TimelineDiagnoseVisible(t *testing.T)
```

### Step 3: Test helpers
Each test should:
1. Create a temp directory with `t.TempDir()`
2. Initialize a git repo (`git init`, `git add`, `git commit`)
3. Run the git-agent binary as a subprocess
4. Assert exit code, stdout, stderr

### Step 4: Run tests (must FAIL)
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./e2e/... -run "TestCapture|TestTimeline|TestDiagnose|TestHelp_Capture" -count=1
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./e2e/... -run "TestCapture|TestTimeline|TestDiagnose|TestHelp_Capture" -count=1 -v
go vet ./e2e/...
```

## Success Criteria
- All 7 BDD scenarios have corresponding test functions
- Tests follow existing e2e pattern (subprocess invocation, not library calls)
- Tests compile with `go vet ./e2e/...`
- Tests create isolated temp git repos (no interference between tests)
- Tests verify exit codes, stdout content, and stderr content
- Tests FAIL until all P1b commands are fully wired (Red phase)
- `TestCapture_ExitsZero_*` tests verify capture never returns non-zero
- `TestTimeline_ShowsCapturedActions` verifies end-to-end capture-then-query flow
- `TestDiagnose_PrintsStubMessage` verifies stderr output and empty stdout
