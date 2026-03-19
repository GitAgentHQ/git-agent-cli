# Task 016: Integration tests

**depends-on**: task-006, task-008, task-010, task-012, task-014

## Description

Write end-to-end integration tests that verify the full CLI workflow with real git repositories and mock HTTP servers.

## Execution Context

**Task Number**: 16 of 16
**Phase**: Testing
**Prerequisites**: All feature implementations complete

## Integration Test Scenarios

### 1. Full commit flow with mock LLM
- Create temp git repo
- Stage files
- Start mock LLM server
- Run `ga commit`
- Verify git log shows commit

### 2. ga init flow with mock LLM
- Create temp git repo with commit history
- Start mock LLM server
- Run `ga init`
- Verify .ga/config.yml created
- Verify .ga/hooks/pre-commit created and executable

### 3. Hook blocking integration
- Create temp repo
- Create pre-commit hook that exits 1
- Run `ga commit`
- Verify exit code 2
- Verify no commit created

### 4. Dry-run integration
- Create temp repo
- Stage files
- Run `ga commit --dry-run`
- Verify commit NOT created
- Verify stdout contains outline

### 5. Verbose mode integration
- Create temp repo
- Stage files
- Run `ga commit --verbose`
- Verify stderr contains debug info

## Files to Modify/Create

- Create: `e2e/init_test.go`
- Create: `e2e/commit_test.go`

## Steps

### Step 1: Set up test helpers
- Create temp git repo helper
- Create mock HTTP server helper
- Create test fixtures

### Step 2: Write ga init integration tests
- Test full init flow
- Test init with --hook conventional
- Test init with --force

### Step 3: Write ga commit integration tests
- Test full commit flow
- Test hook blocking
- Test dry-run

### Step 4: Run integration tests

## Verification Commands

```bash
go test ./e2e/... -v
```

## Success Criteria

- All integration tests pass
- Real git operations verified
- Full CLI workflow verified
- No flaky tests
