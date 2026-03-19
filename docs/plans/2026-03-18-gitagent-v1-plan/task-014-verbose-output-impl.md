# Task 014: Verbose mode and output contract implementation

**depends-on**: task-013

## Description

Implement verbose debug output and ensure proper stdout/stderr separation.

## Execution Context

**Task Number**: 14 of 16
**Phase**: Core Features
**Prerequisites**: Verbose tests written

## Implementation Components

### 1. Verbose output in CommitService
Add verbose logging at key points:
- "resolved model: {model}"
- "resolved api-key: {masked_key}" (show first 4 + last 4 chars)
- "staged files: {files}"
- "diff lines: {count} (within limit)" or "diff truncated: {old} → {new}"
- "calling LLM..."
- "LLM response received"

### 2. Output separation
- stdout: Only commit outline on success
- stdout: Empty on error
- stderr: All debug info (verbose mode)
- stderr: All warnings (diff truncation)
- stderr: All errors

### 3. --verbose flag integration
- Parse --verbose in commit command
- Pass to CommitService
- Enable/disable verbose logging

## Files to Modify/Create

- Modify: `application/commit_service.go`
- Modify: `cmd/commit.go`

## Steps

### Step 1: Add verbose field to CommitInput
- Add Verbose bool to CommitInput DTO

### Step 2: Implement verbose logging in CommitService
- Add verbose logging at each key step
- Mask API key in output

### Step 3: Ensure output separation
- stdout = only outline
- stderr = all else

### Step 4: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./cmd/... -v -run TestVerbose
go test ./application/... -v
```

## Success Criteria

- All verbose tests pass
- Proper stdout/stderr separation
- Correct masking of sensitive data
- Clean machine-readable stdout for agents
