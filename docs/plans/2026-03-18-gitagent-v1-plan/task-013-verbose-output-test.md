# Task 013: Verbose mode and output contract test

**depends-on**: task-008

## Description

Write tests for verbose debug output and stdout/stderr separation.

## Execution Context

**Task Number**: 13 of 16
**Phase**: Core Features
**Prerequisites**: Commit core implementation

## BDD Scenarios

```gherkin
Scenario: Verbose flag outputs debug info to stderr
  Given I have staged changes and GA_API_KEY is set
  When I run `ga commit --verbose`
  Then stderr prints "resolved model: gpt-4o"
  And stderr prints "resolved api-key: sk-1234...abcd" (masked)
  And stderr prints "staged files: [src/main.go]"
  And stderr prints "diff lines: 42 (within limit)"
  And stderr prints "calling LLM..."
  And stderr prints "LLM response received"
  And the commit proceeds normally
  And stdout contains only the outline
  And exit code is 0

Scenario: Verbose flag shows truncation info
  Given I have staged changes totaling 800 lines
  When I run `ga commit --verbose`
  Then stderr includes "diff truncated: 800 → 500 lines"
  And exit code is 0

Scenario: stdout contains only the outline on success
  Given a successful `ga commit` run
  Then stdout contains exactly the outline text and nothing else
  And stdout does not contain the commit message
  And stdout does not contain debug info

Scenario: All errors and warnings go to stderr
  Given any warning (e.g., diff truncation) occurs
  Then the warning is written to stderr
  And stdout is not affected

Scenario: Upstream agent can parse stdout directly
  Given ga is invoked by a Coding Agent subprocess
  And the commit succeeds
  When the agent reads stdout
  Then it receives the clean outline string without extra formatting
```

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Verbose Mode and Output Contract sections)

## Files to Modify/Create

- Create: `cmd/verbose_test.go` (or add to existing test files)

## Steps

### Step 1: Write tests for verbose output
- Test stderr contains debug info when --verbose
- Test stdout does not contain debug info

### Step 2: Write tests for output contract
- Test stdout = only outline on success
- Test stderr = all errors/warnings
- Test stdout = empty on error

### Step 3: Run tests (should fail or need updates)

## Verification Commands

```bash
go test ./cmd/... -v -run TestVerbose
go test ./cmd/... -v -run TestOutput
```

## Success Criteria

- Tests cover all verbose scenarios
- Tests verify stdout/stderr separation
- Tests fail indicating implementation needed
