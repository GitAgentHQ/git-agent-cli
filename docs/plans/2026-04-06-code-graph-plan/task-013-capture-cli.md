# Task 013: capture CLI command

**depends-on**: 012

## Description
Wire `capture` as a flat top-level hidden Cobra command on `rootCmd`. The command wraps all errors to always exit 0 (never blocks agent hooks). No output on success (agents may parse stdout). Warnings go to stderr only.

## Execution Context
**Task Number**: 013 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Capture service (012) must be complete

## BDD Scenario
```gherkin
Feature: capture CLI command
    As an agent hook
    I want a hidden capture command that records actions
    So that tool calls are tracked without disrupting the agent

    Background:
        Given the git-agent binary is built
        And I am in a git repository

    Scenario: capture command is hidden
        When I run "git-agent --help"
        Then "capture" should NOT appear in the help output
        But "git-agent capture --help" should still work

    Scenario: capture records an action and exits 0
        Given the working directory has uncommitted changes
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the command should exit with code 0
        And no output should appear on stdout
        And .git-agent/graph.db should contain a session and action

    Scenario: capture with no changes exits 0
        Given the working directory has no changes
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the command should exit with code 0
        And no output should appear on stdout

    Scenario: capture with errors exits 0
        Given the graph database is corrupted
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the command should exit with code 0
        And stderr should contain a warning message

    Scenario: capture requires --source flag
        When I run "git-agent capture --tool Edit"
        Then the command should exit with non-zero code
        And stderr should indicate --source is required

    Scenario: capture uses $PPID as default instance-id
        When I run "git-agent capture --source claude-code --tool Edit"
        Then the session should use the parent PID as instance_id

    Scenario: capture --end-session ends active session
        Given an active session exists for source "claude-code"
        When I run "git-agent capture --source claude-code --end-session"
        Then the active session should be ended
        And the command should exit with code 0
```

## Files to Modify/Create
- `cmd/capture.go` -- new Cobra command file

## Steps
### Step 1: Create cmd/capture.go
Define `captureCmd` as a Cobra command registered on `rootCmd`:
```go
var captureCmd = &cobra.Command{
    Use:    "capture",
    Short:  "Record agent action into the code graph",
    Hidden: true,
    RunE:   runCapture,
}
```

### Step 2: Define flags
- `--source` (string, required) -- agent source identifier
- `--tool` (string, optional) -- tool name (Edit, Write, Bash, etc.)
- `--instance-id` (string, default: `$PPID`) -- distinguishes concurrent agents
- `--message` (string, optional) -- human-readable description
- `--end-session` (bool, default: false) -- end the active session

### Step 3: Implement runCapture
1. Resolve repo root via `git rev-parse --show-toplevel` or existing helper
2. Create SQLite client pointing to `.git-agent/graph.db`
3. Open + InitSchema (creates DB if missing)
4. Create `GraphClient` and `CaptureService`
5. Build `CaptureRequest` from flags
6. Call `CaptureService.Capture()`
7. Wrap ALL errors: print warning to stderr, return nil (exit 0)
8. On success: no output (silent for agent compatibility)

### Step 4: Register on rootCmd
In `init()`:
```go
rootCmd.AddCommand(captureCmd)
```

### Step 5: Build and test
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent capture --help
./git-agent --help | grep capture  # should NOT appear
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent capture --help                    # help works despite hidden
./git-agent --help | grep -c capture          # 0 (hidden)
./git-agent capture --source test --tool Edit # exits 0
echo $?                                       # 0
```

## Success Criteria
- `captureCmd.Hidden = true` -- does not appear in `--help`
- `--source` is a required flag
- `--instance-id` defaults to `$PPID` (os.Getppid)
- ALL errors are caught and printed to stderr; command always exits 0
- No stdout output on success (agents may parse stdout)
- Creates `.git-agent/graph.db` if it does not exist
- Correctly wires to `CaptureService.Capture()`
- `--end-session` flag calls session ending logic
- `make build` succeeds
- `go vet ./cmd/...` passes
