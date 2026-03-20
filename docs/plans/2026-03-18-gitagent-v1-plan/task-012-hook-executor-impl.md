# Task 012: Hook executor implementation

**depends-on**: task-011

## Description

Implement the hook execution system that runs .git-agent/hooks/pre-commit with JSON payload.

## Execution Context

**Task Number**: 12 of 16
**Phase**: Core Features
**Prerequisites**: Hook executor tests written

## Implementation Components

### 1. infrastructure/hook/executor.go
Implement HookExecutor interface:
- Check if .git-agent/hooks/pre-commit exists
- Check if file is executable (chmod +x)
- Execute hook with JSON via stdin
- Capture stdout/stderr
- Return HookResult with exit code and stderr

### 2. infrastructure/hook/discovery.go
- Check if hook file exists
- Check if hooks directory exists

### 3. domain/hook/input.go
Define HookInput struct:
```go
type HookInput struct {
    Diff          string        `json:"diff"`
    CommitMessage string        `json:"commit_message"`
    Intent        string        `json:"intent"`
    StagedFiles   []string      `json:"staged_files"`
    Config        *ProjectConfig `json:"config,omitempty"`
}
```

### 4. domain/hook/result.go
Define HookResult struct:
```go
type HookResult struct {
    ExitCode int
    Stderr   string
}
```

## Files to Modify/Create

- Modify: `domain/hook/input.go`
- Modify: `domain/hook/result.go`
- Create: `infrastructure/hook/executor.go`
- Create: `infrastructure/hook/discovery.go`

## Steps

### Step 1: Implement hook discovery
- Check file existence
- Check executable permission

### Step 2: Implement hook executor
- Execute hook as subprocess
- Pass JSON via stdin
- Capture stderr
- Return result

### Step 3: Handle special cases
- No hook exists: skip, return nil
- Not executable: return error with exit code 2

### Step 4: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./domain/hook/... -v
go test ./infrastructure/hook/... -v
```

## Success Criteria

- All hook tests pass
- Correct exit codes (0 = proceed, non-zero = block with exit 2)
- Proper JSON payload format
- Error handling for non-executable hooks
