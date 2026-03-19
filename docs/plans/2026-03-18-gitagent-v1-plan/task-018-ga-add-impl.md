# Task 018: ga add command implementation

**depends-on**: task-017

## Description

Implement the `ga add` command and AddService logic to pass tests created in task 017.

## Execution Context

**Task Number**: 18 of 18
**Phase**: Staging Integration
**Prerequisites**: ga add test scenarios written

## Files to Modify/Create

- Create: `application/add_service.go`
- Create: `cmd/add.go`
- Update: `cmd/root.go`

## Steps

### Step 1: Implement AddService
- Implement `Execute` method handling path arguments and delegating to git client
- Return domain results

### Step 2: Wire up cobra command
- Create the `addCmd` in `cmd/add.go`
- Connect it to `rootCmd`
- Map arguments to AddService input

### Step 3: Run tests (should pass)

## Verification Commands

```bash
go test ./cmd/... -v -run TestAdd
go test ./application/... -v -run TestAdd
```

## Success Criteria

- All `ga add` tests from task 017 pass
- `ga add` works as a functional wrapper over `git add`
