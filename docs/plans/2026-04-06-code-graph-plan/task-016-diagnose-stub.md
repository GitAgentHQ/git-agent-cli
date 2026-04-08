# Task 016: diagnose stub command

**depends-on**: none

## Description
Create a `diagnose` stub command as a flat top-level Cobra command on `rootCmd`. The command prints "not yet implemented" to stderr and exits 0. This is a P2 placeholder that can be implemented independently of all other tasks.

## Execution Context
**Task Number**: 016 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: None (independent, can be done anytime)

## BDD Scenario
```gherkin
Feature: diagnose stub command
    As a developer
    I want a diagnose command placeholder
    So that the CLI surface is complete for P1b

    Scenario: diagnose prints stub message
        When I run "git-agent diagnose 'some bug description'"
        Then stderr should contain "not yet implemented"
        And stdout should be empty
        And the exit code should be 0

    Scenario: diagnose appears in help
        When I run "git-agent --help"
        Then "diagnose" should appear in the help output

    Scenario: diagnose accepts a description argument
        When I run "git-agent diagnose 'test failures in auth module'"
        Then the command should accept the argument without error
        And still print "not yet implemented" to stderr
        And exit with code 0
```

## Files to Modify/Create
- `cmd/diagnose.go` -- new Cobra command file

## Steps
### Step 1: Create cmd/diagnose.go
```go
package cmd

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
    Use:   "diagnose [description]",
    Short: "Trace a bug back to the agent action that introduced it",
    Args:  cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Fprintln(os.Stderr, "not yet implemented")
        return nil
    },
}

func init() {
    rootCmd.AddCommand(diagnoseCmd)
}
```

### Step 2: Build and verify
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent diagnose "test bug" 2>&1
./git-agent --help | grep diagnose
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
./git-agent --help | grep diagnose                    # visible in help
./git-agent diagnose "test" 2>/dev/null; echo $?      # exit 0
./git-agent diagnose "test" 2>&1 1>/dev/null          # "not yet implemented" on stderr
go vet ./cmd/...
```

## Success Criteria
- `diagnose` appears in `git-agent --help` (not hidden)
- Accepts 0 or 1 positional argument (bug description)
- Prints "not yet implemented" to stderr
- Produces no stdout output
- Always exits 0
- `make build` succeeds
- `go vet ./cmd/...` passes
- No dependencies on any other graph infrastructure
