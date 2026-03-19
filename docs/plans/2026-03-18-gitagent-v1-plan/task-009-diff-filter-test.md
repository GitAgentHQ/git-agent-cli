# Task 009: Diff filter test

**depends-on**: task-002

## Description

Write tests for diff filtering that excludes lock files and binary files from LLM payload.

## Execution Context

**Task Number**: 9 of 16
**Phase**: Core Features
**Prerequisites**: Domain interfaces defined

## BDD Scenarios

```gherkin
Scenario: Lock files are excluded from LLM payload
  Given I have staged changes including package-lock.json and src/main.go
  When I run `ga commit`
  Then the diff sent to LLM excludes package-lock.json content
  And the diff includes src/main.go content
  And the commit still includes all staged files (lock file is committed)
  And exit code is 0

Scenario: Binary files are excluded from LLM payload
  Given I have staged changes including assets/logo.png and src/app.go
  When I run `ga commit`
  Then the diff sent to LLM excludes binary file content
  And the commit includes both files
  And exit code is 0

Scenario: Diff exceeds max-diff-lines is truncated
  Given I have staged changes totaling 800 lines
  And GA_MAX_DIFF_LINES is not set (default: 500)
  When I run `ga commit`
  Then only 500 lines of diff are sent to the LLM
  And stderr prints "warning: diff truncated to 500 lines (was 800)"
  And a commit is created successfully
  And exit code is 0

Scenario: Custom max-diff-lines via flag
  Given I have staged changes totaling 300 lines
  When I run `ga commit --max-diff-lines 100`
  Then only 100 lines of diff are sent to the LLM
  And exit code is 0

Scenario: All staged files are lock/binary (nothing to send LLM)
  Given I have only staged go.sum and *.png files
  When I run `ga commit`
  Then stderr prints "error: no staged text changes after filtering"
  And no LLM call is made
  And exit code is 1
```

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Diff Filtering and Truncation section)

## Files to Modify/Create

- Create: `pkg/filter/patterns.go` (test first)
- Create: `domain/diff/filter_test.go`

## Steps

### Step 1: Define filter patterns
- Lock files: package-lock.json, yarn.lock, pnpm-lock.yaml, go.sum, Gemfile.lock, etc.
- Binary files: *.png, *.jpg, *.gif, *.ico, *.pdf, etc.

### Step 2: Write tests for filter
- Test lock file exclusion
- Test binary file exclusion
- Test filter with mixed files
- Test empty diff after filtering

### Step 3: Write tests for truncator
- Test truncation with default limit
- Test truncation with custom limit
- Test no truncation when under limit
- Test truncation warning output

### Step 4: Run tests (should fail)

## Verification Commands

```bash
go test ./domain/diff/... -v
go test ./pkg/filter/... -v
```

## Success Criteria

- Tests cover all filtering scenarios
- Tests cover all truncation scenarios
- Tests fail indicating missing implementation
