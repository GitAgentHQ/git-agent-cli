# Task 017: Action Capture Test

**depends-on**: task-003, task-004

## Description

Write tests for the action capture service: recording agent/human actions with diffs, session lifecycle (create, reuse, timeout), edge cases (no diff, truncation, custom message, end session, lock contention, missing DB).

## Execution Context

**Task Number**: 017 of 020 (test)
**Phase**: P1 - Action Tracking
**Prerequisites**: KuzuDB client (task-003) and git client (task-004)

## BDD Scenario

```gherkin
Scenario: Capture an agent edit action
  Given I modified "src/main.go" with an Edit tool
  And the modification adds 3 lines and removes 1 line
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then a Session node should exist with source "claude-code"
  And an Action node should be created with tool "Edit"
  And the Action should contain the unified diff
  And an ACTION_MODIFIES edge should link the Action to "src/main.go"
  And the edge should have additions=3 and deletions=1

Scenario: Capture appends to existing active session
  Given a Session "s1" exists with source "claude-code" started 5 minutes ago
  And "s1" already has 2 actions
  When I run "git-agent graph capture --source claude-code --tool Write"
  Then the Action should be added to Session "s1" (not a new session)
  And the Action id should be "s1:3"

Scenario: Capture creates new session after timeout
  Given a Session "s1" exists with source "claude-code" started 45 minutes ago
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then a new Session "s2" should be created
  And "s1" should have ended_at set automatically

Scenario: Capture with no diff is a no-op
  Given the working directory has no uncommitted changes
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then no Action node should be created
  And the output should indicate skipped with reason "no changes detected"

Scenario: Capture truncates large diffs
  Given I modified a file producing a diff larger than 100KB
  When I run "git-agent graph capture --source claude-code --tool Bash"
  Then the stored diff should be truncated at 100KB
  And the diff should end with "[truncated]"

Scenario: Capture with custom message
  When I run "git-agent graph capture --source human --message 'fixed auth bug'"
  Then the Action node should have message "fixed auth bug"

Scenario: End session explicitly
  Given a Session "s1" exists with source "claude-code"
  When I run "git-agent graph capture --source claude-code --end-session"
  Then Session "s1" should have ended_at set to now

Scenario: Capture skips silently when DB is locked
  Given another process holds ".git-agent/graph.lock"
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then the command should exit with code 0
  And stderr should contain a warning about lock contention

Scenario: Capture without prior index creates graph DB
  Given the repository has no existing graph database
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then a graph database should be created at ".git-agent/graph.db"
  And the Session and Action nodes should be stored
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Action Capture)

## Files to Modify/Create

- Create: `application/graph_capture_service_test.go` (with `//go:build graph` tag)

## Steps

### Step 1: Write basic capture tests

- `TestCaptureService_Capture_Basic`: Verify Session and Action node creation, ACTION_MODIFIES edge with additions/deletions
- `TestCaptureService_Capture_SessionReuse`: Verify active session is reused (not a new one)
- `TestCaptureService_Capture_ActionSequence`: Verify action ID is `{session_id}:{sequence}`

### Step 2: Write session lifecycle tests

- `TestCaptureService_Capture_SessionTimeout`: After 30-minute timeout, new session created, old session ended
- `TestCaptureService_EndSession`: Explicit end-session sets ended_at

### Step 3: Write edge case tests

- `TestCaptureService_Capture_NoDiff`: No changes detected, returns skipped result
- `TestCaptureService_Capture_TruncateLargeDiff`: Diff > 100KB is truncated with "[truncated]" marker
- `TestCaptureService_Capture_CustomMessage`: Message field populated in Action node
- `TestCaptureService_Capture_LockContention`: Exits 0 with warning when lock is held
- `TestCaptureService_Capture_NoExistingDB`: Creates graph DB on first capture

### Step 4: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestCaptureService` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestCaptureService -v
```

## Success Criteria

- Tests cover all 9 action capture scenarios
- Session lifecycle (create, reuse, timeout, end) tested
- Edge cases (no diff, truncation, lock, missing DB) tested
- All tests FAIL (Red phase)
