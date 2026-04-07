# Task 018: Timeline Query Impl

**depends-on**: task-018-timeline-test

## Description

Implement the timeline query in CaptureService: list sessions and actions with filtering, and optionally compress sessions into LLM-generated summaries.

## Execution Context

**Task Number**: 018 of 020 (impl)
**Phase**: P1 - Action Tracking
**Prerequisites**: Failing tests from task-018-timeline-test

## BDD Scenario

```gherkin
Scenario: Timeline shows raw actions (offline)
  When I run "git-agent graph timeline --since 2h"
  Then the output should list all sessions with actions
  And no summary field should be populated
  And the command should not make any LLM calls

Scenario: Timeline with compression (requires LLM)
  When I run "git-agent graph timeline --since 2h --compress"
  Then each session should have a summary field
  And individual actions should not be listed
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Timeline)

## Files to Modify/Create

- Modify: `application/graph_capture_service.go` -- add Timeline method
- Modify: `infrastructure/graph/sqlite_repository.go` -- implement Timeline SQL query

## Steps

### Step 1: Implement Timeline SQL query

Query sessions with their actions, filtered by:
- `Since`: session.started_at >= timestamp
- `Source`: session.source = value
- `File`: join through ACTION_MODIFIES to filter by file path

Return sessions ordered by started_at descending.

### Step 2: Implement raw mode

Return full session + action data. No LLM calls. Summary fields are null.

### Step 3: Implement compressed mode

When `Compress` flag is set:
1. Group actions by session
2. For each session, build a prompt with all action diffs
3. Call LLM to generate a human-readable summary
4. Return sessions with summary filled, actions omitted, action_count set

### Step 4: Handle empty result

When no sessions match, return `TimelineResult{Sessions: [], TotalSessions: 0, TotalActions: 0}`.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test ./application/... -run TestCaptureService_Timeline` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./application/... -run TestCaptureService_Timeline -v
```

## Success Criteria

- Raw timeline returns sessions with full action data
- Compressed timeline calls LLM and returns summaries
- All filters (source, file, time) work
- Empty result handled correctly
- All timeline tests pass (Green)
