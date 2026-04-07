# Task 017: Action Capture Impl

**depends-on**: task-017-capture-test

## Description

Implement the CaptureService for recording agent/human actions: read git diff, manage session lifecycle (create/reuse/timeout), create Action nodes with diffs and metadata, handle edge cases (no diff, truncation, lock contention, missing DB).

## Execution Context

**Task Number**: 017 of 020 (impl)
**Phase**: P1 - Action Tracking
**Prerequisites**: Failing tests from task-017-capture-test

## BDD Scenario

```gherkin
Scenario: Capture an agent edit action
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then a Session node should exist with source "claude-code"
  And an Action node should be created with tool "Edit"
  And the Action should contain the unified diff
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Action Capture)

## Files to Modify/Create

- Create: `application/graph_capture_service.go` (with `//go:build graph` tag)

## Steps

### Step 1: Create CaptureService struct

Define `CaptureService` with GraphRepository and GraphGitClient dependencies. Constructor function `NewCaptureService(...)`.

### Step 2: Implement Capture method

Follow the capture algorithm from the design:
1. Read git diff (unstaged + staged)
2. If empty, return skipped result (exit 0)
3. Open/create DB, init schema
4. Find or create session (query active session for source within timeout)
5. Create Action node with sequential ID, tool, diff, timestamp
6. Create SESSION_CONTAINS and ACTION_MODIFIES edges
7. Parse diff headers for changed files and addition/deletion counts

### Step 3: Implement session lifecycle

- Active session lookup: `GetActiveSession(source, timeoutMinutes=30)`
- Timeout: if last action > 30min ago, end old session and create new one
- End session: `EndSession(sessionID)` sets ended_at

### Step 4: Handle edge cases

- Diff truncation at 100KB with "[truncated]" marker
- Lock contention: try lock with short timeout (100ms), exit 0 with stderr warning if held
- Missing DB: create graph.db and init schema before storing

### Step 5: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./application/... -run TestCaptureService` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./application/... -run TestCaptureService -v
```

## Success Criteria

- Capture creates Session and Action nodes correctly
- Session lifecycle (create, reuse, timeout) works
- Diff truncation at 100KB works
- Lock contention handled gracefully (exit 0)
- Performance target: < 200ms total
- All capture tests pass (Green)
