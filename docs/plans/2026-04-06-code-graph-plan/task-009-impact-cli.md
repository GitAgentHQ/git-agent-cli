# Task 009: Impact CLI command

**depends-on**: task-006, task-008

## Description
Wire `impact` as a flat top-level Cobra command on `rootCmd` (NOT under a `graph` namespace). Supports TTY-aware output (text for terminal, JSON when piped), with `--json` and `--text` overrides. Auto-indexes via EnsureIndex with progress on stderr.

## Execution Context
**Task Number**: 009 of 018
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-006 (EnsureIndex), task-008 (ImpactService)

## BDD Scenario
```gherkin
Feature: impact CLI command

  Scenario: impact command is a flat top-level command
    Given git-agent is built
    When I run git-agent --help
    Then "impact" appears as a top-level command
    And there is no "graph" namespace or subcommand

  Scenario: impact shows text output in terminal
    Given a git repository with indexed history
    And stdout is a TTY
    When I run git-agent impact application/commit_service.go
    Then output is human-readable text with file paths and strengths
    And output goes to stdout

  Scenario: impact shows JSON output when piped
    Given a git repository with indexed history
    And stdout is NOT a TTY (piped)
    When I run git-agent impact application/commit_service.go
    Then output is valid JSON

  Scenario: --json flag forces JSON output in terminal
    Given stdout is a TTY
    When I run git-agent impact application/commit_service.go --json
    Then output is valid JSON

  Scenario: --text flag forces text output when piped
    Given stdout is NOT a TTY
    When I run git-agent impact application/commit_service.go --text
    Then output is human-readable text

  Scenario: Auto-index on first run
    Given a git repository with no graph.db
    When I run git-agent impact some_file.go
    Then progress messages appear on stderr
    And graph.db is created
    And impact results are shown on stdout

  Scenario: --reindex forces full re-index
    Given a git repository with existing graph.db
    When I run git-agent impact some_file.go --reindex
    Then graph.db is dropped and recreated
    And fresh impact results are shown

  Scenario: Flags are respected
    When I run git-agent impact --help
    Then flags include --depth (default 2), --top (default 20), --min-count (default 3)
    And flags include --reindex, --json, --text
```

## Files to Modify/Create
- `cmd/impact.go` -- Cobra command definition and wiring

## Steps
### Step 1: Create `cmd/impact.go`
```go
var impactCmd = &cobra.Command{
    Use:   "impact <file>",
    Short: "Show files that frequently change with the target file",
    Args:  cobra.ExactArgs(1),
    RunE:  runImpact,
}
```

### Step 2: Register on rootCmd
In `init()`:
```go
rootCmd.AddCommand(impactCmd)
impactCmd.Flags().IntP("depth", "d", 2, "transitive depth")
impactCmd.Flags().IntP("top", "t", 20, "max results")
impactCmd.Flags().Int("min-count", 3, "minimum co-change count")
impactCmd.Flags().Bool("reindex", false, "force full re-index")
impactCmd.Flags().Bool("json", false, "force JSON output")
impactCmd.Flags().Bool("text", false, "force text output")
```

### Step 3: Implement runImpact
1. Resolve repo path (current directory or flag)
2. Create EnsureIndexService, run EnsureIndex (progress to stderr)
3. Create ImpactService
4. Build ImpactRequest from flags and args
5. Call Query
6. Determine output format: `--json` -> JSON, `--text` -> text, else `term.IsTerminal(os.Stdout.Fd())`
7. Render output

### Step 4: Implement text renderer
```
Impact for: application/commit_service.go

  service_test.go        85%  (15 co-changes)
  model.go               53%  (8 co-changes)
  routes.go              30%  (4 co-changes)
```

### Step 5: Implement JSON renderer
```json
{
  "target": "application/commit_service.go",
  "co_changed": [
    {"file": "service_test.go", "strength": 0.85, "co_count": 15},
    ...
  ],
  "rename_chain": []
}
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent impact --help                    # command exists, shows flags
./git-agent impact cmd/root.go               # text output
./git-agent impact cmd/root.go --json        # JSON output
./git-agent impact cmd/root.go | cat         # JSON when piped
./git-agent impact cmd/root.go --text | cat  # text when piped with override
```

## Success Criteria
- `impact` is a flat top-level command (no `graph` namespace)
- `rootCmd.AddCommand(impactCmd)` in `cmd/impact.go`
- Flags: `--depth 2`, `--top 20`, `--min-count 3`, `--reindex`, `--json`, `--text`
- TTY-aware: text in terminal, JSON when piped
- `--json` and `--text` override auto-detection
- Auto-indexes via EnsureIndex (progress on stderr)
- `make build` succeeds
- `make test` passes
