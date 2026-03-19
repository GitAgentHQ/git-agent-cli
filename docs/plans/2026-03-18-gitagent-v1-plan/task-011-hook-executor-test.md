# Task 011: Hook executor test

**depends-on**: task-002

## Description

Write tests for the hook execution system that runs .ga/hooks/pre-commit with JSON payload.

## Execution Context

**Task Number**: 11 of 16
**Phase**: Core Features
**Prerequisites**: Domain interfaces defined

## BDD Scenarios

```gherkin
Scenario: Pre-commit hook passes validation
  Given I have staged changes
  And .ga/hooks/pre-commit is an executable script that exits 0
  When I run `ga commit`
  Then the hook is executed with JSON payload via stdin
  And the JSON payload matches: {diff, commit_message, intent, staged_files}
  And the commit proceeds
  And exit code is 0

Scenario: Pre-commit hook blocks commit
  Given I have staged changes
  And .ga/hooks/pre-commit exits with code 1
  And the hook writes "error: WIP commits not allowed" to stderr
  When I run `ga commit`
  Then `git commit` is NOT executed
  And stderr prints the hook's error message
  And exit code is 2

Scenario: Pre-commit hook does not exist
  Given I have staged changes
  And .ga/hooks/pre-commit does not exist
  When I run `ga commit`
  Then no hook is executed
  And the commit proceeds normally
  And exit code is 0

Scenario: .ga/hooks directory does not exist
  Given I have staged changes
  And the .ga/hooks directory does not exist
  When I run `ga commit`
  Then no hook execution is attempted
  And the commit proceeds normally
  And exit code is 0

Scenario: Pre-commit hook is not executable
  Given I have staged changes
  And .ga/hooks/pre-commit exists but has no execute permission (chmod 644)
  When I run `ga commit`
  Then stderr prints "error: hook is not executable: .ga/hooks/pre-commit"
  And `git commit` is NOT executed
  And exit code is 2

Scenario: Hook receives correct JSON schema
  Given I have staged changes to ["src/main.go", "src/cache.go"]
  And I run `ga commit --intent "add caching"`
  And .ga/hooks/pre-commit captures and validates stdin
  Then the hook stdin JSON contains:
    - diff: (non-empty filtered diff)
    - commit_message: (full assembled message: title + blank line + body + optional Co-Authored-By)
    - intent: "add caching"
    - staged_files: ["src/main.go", "src/cache.go"]
```

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Hook System section)

## Files to Modify/Create

- Create: `domain/hook/executor_test.go`

## Steps

### Step 1: Write tests for hook executor
- Test hook execution with exit 0
- Test hook execution with exit non-zero
- Test no hook exists scenario
- Test non-executable hook

### Step 2: Write tests for JSON payload
- Test payload structure
- Test payload includes all fields

### Step 3: Run tests (should fail)

## Verification Commands

```bash
go test ./domain/hook/... -v
go test ./infrastructure/hook/... -v
```

## Success Criteria

- Tests cover all hook scenarios
- Tests verify JSON payload structure
- Tests fail indicating missing implementation
