# Task 018: Timeline Query Test

**depends-on**: task-017

## Description

Write tests for the timeline query: listing sessions and actions, filtering by source/file/time, LLM-compressed summaries, and empty timeline handling.

## Execution Context

**Task Number**: 018 of 020 (test)
**Phase**: P1 - Action Tracking
**Prerequisites**: CaptureService (task-017) providing session/action data

## BDD Scenario

```gherkin
Scenario: Timeline shows raw actions (offline)
  When I run "git-agent graph timeline --since 2h"
  Then the output should list all 3 sessions
  And each session should include its actions with diffs
  And no summary field should be populated
  And the command should not make any LLM calls

Scenario: Timeline filtered by source
  When I run "git-agent graph timeline --source claude-code"
  Then only sessions s1 and s3 should appear
  And session s2 (human) should not appear

Scenario: Timeline filtered by file
  Given action s1:2 modified "src/main.go"
  And action s3:1 modified "src/main.go"
  When I run "git-agent graph timeline --file src/main.go"
  Then only sessions containing actions that touched "src/main.go" should appear
  And within each session, only matching actions should be shown

Scenario: Timeline with compression (requires LLM)
  When I run "git-agent graph timeline --since 2h --compress"
  Then each session should have a summary field with a human-readable description
  And individual actions should not be listed (only action_count)
  And the LLM should be called with grouped diffs for each session

Scenario: Timeline with time range
  When I run "git-agent graph timeline --since 2026-04-06T14:30:00Z"
  Then only session s3 should appear (started after the cutoff)

Scenario: Empty timeline
  Given no sessions exist in the graph
  When I run "git-agent graph timeline"
  Then the output should show empty sessions array
  And total_sessions and total_actions should be 0
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Timeline)

## Files to Modify/Create

- Modify: `application/graph_capture_service_test.go` (add timeline test cases)

## Steps

### Step 1: Write raw timeline tests

- `TestCaptureService_Timeline_Raw`: Verify sessions and actions returned without LLM calls, no summary field populated
- `TestCaptureService_Timeline_FilterBySource`: Verify source filter excludes non-matching sessions
- `TestCaptureService_Timeline_FilterByFile`: Verify file filter narrows to actions touching that file

### Step 2: Write compressed timeline tests

- `TestCaptureService_Timeline_Compress`: Mock LLM client, verify each session gets a summary, actions array omitted, action_count present

### Step 3: Write edge case tests

- `TestCaptureService_Timeline_TimeRange`: Verify `Since` filter on session start time
- `TestCaptureService_Timeline_Empty`: Verify empty result structure (sessions=[], total_sessions=0, total_actions=0)

### Step 4: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./application/... -run TestCaptureService_Timeline` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./application/... -run TestCaptureService_Timeline -v
```

## Success Criteria

- Tests cover all 6 non-P2 timeline scenarios
- Raw mode (no LLM) and compressed mode (with LLM mock) tested
- Filters (source, file, time) tested
- Empty timeline edge case tested
- All tests FAIL (Red phase)
