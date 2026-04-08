# Task 012: Capture service test

**depends-on**: 003, 004

## Description
Write tests for the delta-based capture service. Tests use a real SQLite repository (via `setupCaptureTest`) and a mock `GraphGitClient` to verify delta tracking, session management, timeout behavior, and error resilience. All tests must FAIL initially (Red phase) -- the capture service implementation comes in task-012-impl.

## Execution Context
**Task Number**: 012 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: SQLite client lifecycle (003) and git graph client extensions (004) must be complete

## BDD Scenario
```gherkin
Feature: Action Capture Service
    As a coding agent hook
    I want to record each tool call's diff into the graph
    So that fine-grained action history is available for timeline and diagnosis

    Background:
        Given a SQLite graph repository initialized with schema
        And a mock GraphGitClient

    Scenario: Capture agent edit action with delta tracking
        Given I modified "src/main.go" with an Edit tool
        And no prior capture baseline exists for "src/main.go"
        When I call CaptureService.Capture with source "claude-code" and tool "Edit"
        Then a Session node should exist with source "claude-code"
        And an Action node should be created with tool "Edit"
        And the Action should contain the unified diff for "src/main.go"
        And action_modifies rows should link the Action to "src/main.go"
        And the capture_baseline should store the current hash for "src/main.go"
        And CaptureResult.Skipped should be false

    Scenario: Delta capture attributes only newly changed files
        Given I previously captured changes to "a.go" (baseline hash "hash-a-v1" exists)
        And "a.go" still has hash "hash-a-v1" (unchanged)
        And I modified "b.go" with a new hash "hash-b-v1"
        When I call CaptureService.Capture
        Then CaptureResult.FilesChanged should contain only "b.go"
        And the diff should not contain changes from "a.go"
        And the capture_baseline should be updated for both files

    Scenario: Capture appends to existing active session
        Given a session exists with source "claude-code" and instance_id "pid-1" started 5 minutes ago
        And the session already has 1 action
        When I call CaptureService.Capture with source "claude-code" and instance_id "pid-1"
        Then the Action should be added to the existing session
        And the Action ID should end with ":2"

    Scenario: Capture creates new session after timeout
        Given a session exists with source "claude-code" started 31 minutes ago
        When I call CaptureService.Capture with source "claude-code"
        Then a new session should be created (different session ID)
        And the old session should not be reused

    Scenario: Capture with no diff is a no-op (exit 0)
        Given the mock git client returns no changed files
        When I call CaptureService.Capture
        Then CaptureResult.Skipped should be true
        And CaptureResult.Reason should be "no changes detected"
        And no session or action should be created

    Scenario: Concurrent agents use separate sessions via instance_id
        Given a session exists with source "claude-code" and instance_id "1234"
        When I call CaptureService.Capture with instance_id "5678"
        Then a new session should be created for instance_id "5678"
        And the original session for "1234" should remain unchanged

    Scenario: Capture without prior index creates graph DB
        Given a fresh SQLite repository with initialized schema but no prior data
        When I call CaptureService.Capture with changed files
        Then a session and action should be created successfully
        And the capture_baseline should be populated
```

## Files to Modify/Create
- `application/capture_service_test.go` -- test file (already exists with initial tests)

## Steps
### Step 1: Review existing tests
Read the existing `application/capture_service_test.go` to understand what is already covered.

### Step 2: Verify all BDD scenarios have corresponding test functions
Ensure these test functions exist and cover the scenarios above:
- `TestCaptureService_CaptureCreatesSessionAndAction` -- delta tracking with no prior baseline
- `TestCaptureService_DeltaCapture_OnlyNewChanges` -- delta attributes only new files
- `TestCaptureService_AppendsToExistingSession` -- session reuse within timeout
- `TestCaptureService_NewSessionAfterTimeout` -- 30min timeout creates new session
- `TestCaptureService_NoDiff_IsNoOp` -- no changes means skipped
- `TestCaptureService_ConcurrentSessions` -- separate instance_id means separate sessions
- `TestCaptureService_EndSession` -- explicit session ending
- `TestCaptureService_DiffTruncation` -- large diffs truncated at 100KB

### Step 3: Add any missing test cases
Add test for concurrent sessions via instance_id if not already present. Ensure all assertions check `CaptureResult` fields, session IDs, action IDs, and baseline state.

### Step 4: Run tests (must FAIL)
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCaptureService -count=1
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCaptureService -count=1 -v
# All capture tests should exist and compile
# Tests should FAIL if capture_service.go is not yet implemented
```

## Success Criteria
- All 7+ BDD scenarios have corresponding test functions in `capture_service_test.go`
- Tests use real SQLite (via `setupCaptureTest`) and mock `GraphGitClient`
- Tests compile successfully with `go vet ./application/...`
- Tests verify delta-based tracking (hash comparison against capture_baseline)
- Tests verify session scoping by source + instance_id + 30min timeout
- Tests verify no-op behavior when no diff exists
- Tests verify concurrent agent isolation via instance_id
- Target: <200ms for the full test suite to run
