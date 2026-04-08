# Task 015: timeline CLI command

**depends-on**: 014

## Description
Wire `timeline` as a flat top-level Cobra command on `rootCmd`. The command queries sessions and actions with optional filters. Output is TTY-aware: text table for terminal, JSON when piped.

## Execution Context
**Task Number**: 015 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Timeline service (014) must be complete

## BDD Scenario
```gherkin
Feature: timeline CLI command
    As a developer or coding agent
    I want to view a timeline of agent and human actions
    So that I can understand what changes were made and when

    Background:
        Given the git-agent binary is built
        And I am in a git repository with captured actions

    Scenario: timeline command appears in help
        When I run "git-agent --help"
        Then "timeline" should appear in the help output

    Scenario: timeline shows all sessions
        When I run "git-agent timeline"
        Then the output should list sessions with their actions
        And the exit code should be 0

    Scenario: timeline filtered by source
        When I run "git-agent timeline --source claude-code"
        Then only sessions from "claude-code" should appear

    Scenario: timeline filtered by file
        When I run "git-agent timeline --file src/main.go"
        Then only sessions with actions touching "src/main.go" should appear

    Scenario: timeline filtered by time
        When I run "git-agent timeline --since 2h"
        Then only sessions from the last 2 hours should appear

    Scenario: timeline with top limit
        When I run "git-agent timeline --top 5"
        Then at most 5 sessions should appear

    Scenario: timeline outputs JSON when piped
        When I run "git-agent timeline | cat"
        Then the output should be valid JSON
        And contain "sessions", "total_sessions", "total_actions" fields

    Scenario: timeline outputs text table in TTY
        When I run "git-agent timeline" in a terminal
        Then the output should be a human-readable text table
        And include session source, time, and action count

    Scenario: timeline with empty graph
        Given no sessions exist
        When I run "git-agent timeline"
        Then the output should indicate no sessions found
        And the exit code should be 0
```

## Files to Modify/Create
- `cmd/timeline.go` -- new Cobra command file

## Steps
### Step 1: Create cmd/timeline.go
Define `timelineCmd` as a Cobra command registered on `rootCmd`:
```go
var timelineCmd = &cobra.Command{
    Use:   "timeline",
    Short: "Show timeline of agent and human actions",
    RunE:  runTimeline,
}
```

### Step 2: Define flags
- `--since` (string, optional) -- time filter: duration ("2h", "30m") or RFC 3339 timestamp
- `--source` (string, optional) -- filter by source (e.g., "claude-code", "human")
- `--file` (string, optional) -- filter by file path
- `--top` (int, default: 50) -- maximum number of sessions to show
- `--json` (bool, optional) -- force JSON output
- `--text` (bool, optional) -- force text output

### Step 3: Implement runTimeline
1. Resolve repo root
2. Create SQLite client pointing to `.git-agent/graph.db`
3. Open repository (return error if DB does not exist)
4. Parse `--since` flag (support durations like "2h" and RFC 3339 timestamps)
5. Build `TimelineRequest` from flags
6. Call `TimelineService.Query()`
7. Detect TTY (os.Stdout is terminal) for output format
8. If JSON (piped or `--json`): marshal result to JSON
9. If text (TTY or `--text`): render as human-readable table

### Step 4: Implement TTY-aware output
- Text format: table with columns for session source, started_at, action count, and files
- JSON format: direct marshal of `TimelineResult`
- `--json` and `--text` flags override auto-detection

### Step 5: Implement parseSince helper
Parse `--since` value as either:
- Duration string ("2h", "30m", "7d") -- subtract from now
- RFC 3339 timestamp ("2026-04-06T14:00:00Z") -- use directly

### Step 6: Register on rootCmd
In `init()`:
```go
rootCmd.AddCommand(timelineCmd)
```

### Step 7: Build and test
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent timeline --help
./git-agent --help | grep timeline  # should appear
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent timeline --help                         # help works
./git-agent --help | grep timeline                  # visible in help
./git-agent timeline                                # exits 0
./git-agent timeline --since 2h --source claude-code  # filters work
./git-agent timeline --json | jq .                  # valid JSON
go vet ./cmd/...
```

## Success Criteria
- `timeline` appears in `git-agent --help` (not hidden)
- `--since` supports both duration strings and RFC 3339 timestamps
- `--source`, `--file`, `--top` flags filter results correctly
- TTY-aware output: text table for terminal, JSON when piped
- `--json` and `--text` flags override auto-detection
- Exits 0 even with empty timeline
- `make build` succeeds
- `go vet ./cmd/...` passes
