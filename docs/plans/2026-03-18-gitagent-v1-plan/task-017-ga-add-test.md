# Task 017: git agent add command test

**depends-on**: task-002

## Description

Write tests for the `git agent add` command, which acts as a headless wrapper around `git add` to allow full takeover of the staging process.

## Execution Context

**Task Number**: 17 of 18
**Phase**: Staging Integration
**Prerequisites**: Domain interfaces defined

## BDD Scenarios

```gherkin
Scenario: Stage specific files
  Given I have modified a file
  When I run `ga add src/main.go`
  Then `git add src/main.go` is executed
  And exit code is 0

Scenario: Stage all files
  Given I have multiple modified and untracked files
  When I run `ga add .`
  Then `git add .` is executed
  And exit code is 0
```

## Files to Modify/Create

- Create: `cmd/add_test.go`
- Create: `application/add_service_test.go`

## Steps

### Step 1: Create unit tests for AddService
- Test with mock git client ensuring correct paths are passed

### Step 2: Create command tests
- Test flag and argument parsing

### Step 3: Run tests (should fail)

## Verification Commands

```bash
go test ./cmd/... -v -run TestAdd
go test ./application/... -v -run TestAdd
```

## Success Criteria

- Tests cover file staging scenarios
- Tests fail indicating missing implementation
