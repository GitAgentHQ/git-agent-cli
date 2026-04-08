# Task 014: Timeline service test

**depends-on**: 012

## Description
Write tests for the timeline service. Tests use a real SQLite repository with pre-populated sessions and actions. The timeline service queries sessions and actions with filters (since, source, file, top). All tests must FAIL initially (Red phase).

## Execution Context
**Task Number**: 014 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Capture service (012) must be complete (timeline reads capture data)

## BDD Scenario
```gherkin
Feature: Timeline Service
    As a developer or coding agent
    I want to query a timeline of agent and human actions
    So that I can understand what changes were made and when

    Background:
        Given a SQLite graph repository with initialized schema
        And the following sessions and actions are pre-populated:
            | session | source      | instance_id | started_at (relative) | actions |
            | s1      | claude-code | pid-1       | -2h                   | 3       |
            | s2      | human       | pid-2       | -1h40m                | 1       |
            | s3      | claude-code | pid-3       | -1h                   | 2       |

    Scenario: Timeline shows raw actions (offline)
        When I call TimelineService.Query with no filters
        Then TimelineResult should contain all 3 sessions
        And each session should list its actions
        And TotalSessions should be 3
        And TotalActions should be 6
        And no LLM calls should be made

    Scenario: Timeline filtered by source
        When I call TimelineService.Query with source "claude-code"
        Then only sessions s1 and s3 should appear
        And session s2 (human) should not appear
        And TotalSessions should be 2

    Scenario: Timeline filtered by file
        Given action s1:2 has action_modifies for "src/main.go"
        And action s3:1 has action_modifies for "src/main.go"
        When I call TimelineService.Query with file "src/main.go"
        Then only sessions containing actions that touched "src/main.go" should appear

    Scenario: Timeline with time range
        When I call TimelineService.Query with since = 90 minutes ago
        Then only sessions started after the cutoff should appear
        And sessions started before the cutoff should be excluded

    Scenario: Empty timeline
        Given no sessions exist in the graph
        When I call TimelineService.Query
        Then TimelineResult.Sessions should be empty
        And TotalSessions should be 0
        And TotalActions should be 0
```

## Files to Modify/Create
- `application/timeline_service_test.go` -- new test file

## Steps
### Step 1: Create test file with helper
Create `application/timeline_service_test.go` with a helper function that populates the SQLite repository with test sessions and actions.

### Step 2: Implement test functions
- `TestTimelineService_ShowsRawActions` -- all sessions and actions returned
- `TestTimelineService_FilteredBySource` -- only matching source sessions
- `TestTimelineService_FilteredByFile` -- only sessions with actions touching the file
- `TestTimelineService_WithTimeRange` -- since filter excludes old sessions
- `TestTimelineService_Empty` -- no sessions returns zero counts

### Step 3: Pre-populate test data
Use `repo.UpsertSession()` and `repo.CreateAction()` directly to set up test data. Use `repo.CreateActionModifies()` for file-based filtering tests.

### Step 4: Run tests (must FAIL)
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestTimelineService -count=1
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestTimelineService -count=1 -v
go vet ./application/...
```

## Success Criteria
- All 5 BDD scenarios have corresponding test functions
- Tests use real SQLite (via `setupCaptureTest` or similar helper)
- Tests pre-populate sessions and actions directly via repository methods
- Tests verify `TimelineResult` fields: Sessions, TotalSessions, TotalActions
- Tests verify filtering by source, file, and time range
- Tests verify empty timeline returns zero counts
- Tests compile with `go vet ./application/...`
- Tests FAIL until timeline service is implemented (Red phase)
