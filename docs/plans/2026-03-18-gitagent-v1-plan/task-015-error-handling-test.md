# Task 015: Error handling and exit codes test

**depends-on**: task-008, task-010, task-012

## Description

Write tests for error handling scenarios and exit code verification.

## Execution Context

**Task Number**: 15 of 16
**Phase**: Core Features
**Prerequisites**: Core features implemented

## BDD Scenarios

```gherkin
Scenario: Success exits with code 0
  Given a successful `ga commit` run
  Then the process exits with code 0
  And stdout contains the outline

Scenario: General error exits with code 1
  Given any system-level failure (no staged changes, API error, config error)
  Then the process exits with code 1
  And stderr contains a descriptive error message
  And stdout is empty

Scenario: Hook block exits with code 2
  Given a pre-commit hook rejects the commit
  Then the process exits with code 2
  And stderr contains the hook's rejection message
  And stdout is empty

Scenario: No staged changes
  Given I have no staged changes in the repository
  When I run `ga commit`
  Then stderr prints "error: no staged changes to commit"
  And no LLM call is made
  And `git commit` is NOT executed
  And exit code is 1

Scenario: Missing API key
  Given no GA_API_KEY env var is set
  And no --api-key flag is provided
  When I run `ga commit`
  Then stderr prints "error: API key required (set GA_API_KEY or use --api-key)"
  And exit code is 1

Scenario: LLM API returns HTTP error
  Given I have staged changes
  And the OpenAI API returns HTTP 500
  When I run `ga commit`
  Then stderr prints "error: failed to generate commit message: <details>"
  And `git commit` is NOT executed
  And exit code is 1

Scenario: LLM API timeout
  Given I have staged changes
  And the OpenAI API does not respond within 30 seconds
  When I run `ga commit`
  Then stderr prints "error: API request timed out"
  And exit code is 1

Scenario: LLM returns malformed JSON
  Given I have staged changes
  And the LLM returns a response that is not valid JSON
  When I run `ga commit`
  Then stderr prints "error: invalid LLM response format"
  And exit code is 1

Scenario: LLM returns JSON missing commit_message
  Given I have staged changes
  And the LLM returns {"outline": "..."} without commit_message or body
  When I run `ga commit`
  Then stderr prints "error: LLM response missing required field: commit_message"
  And exit code is 1

Scenario: LLM returns JSON missing body
  Given I have staged changes
  And the LLM returns {"commit_message": "feat: x", "outline": "..."} without body
  When I run `ga commit`
  Then stderr prints "error: LLM response missing required field: body"
  And exit code is 1

Scenario: Not in a git repository
  Given the current directory is not a git repository
  When I run `ga commit`
  Then stderr prints "error: not a git repository"
  And no LLM call is made
  And exit code is 1

Scenario: git binary not found
  Given `git` is not installed or not in PATH
  When I run `ga commit`
  Then stderr prints "error: git not found in PATH"
  And no LLM call is made
  And exit code is 1
```

**Spec Source**: `../2026-03-18-gitagent-v1-design/bdd-specs.md` (Environment Error Scenarios, Error Scenarios, Exit Code Contract sections)

## Files to Modify/Create

- Create: `pkg/errors/errors.go`
- Create: `application/error_handling_test.go`

## Steps

### Step 1: Define error types
- ExitCodeError with code and message
- ValidationError
- APIError
- HookBlockedError

### Step 2: Write error handling tests
- Test each error scenario
- Verify exit codes: 0, 1, 2
- Verify stderr messages

### Step 3: Run tests

## Verification Commands

```bash
go test ./application/... -v -run TestError
go test ./pkg/errors/... -v
```

## Success Criteria

- Tests cover all error scenarios
- Correct exit codes for each error type
- Descriptive error messages
- stdout empty on errors
