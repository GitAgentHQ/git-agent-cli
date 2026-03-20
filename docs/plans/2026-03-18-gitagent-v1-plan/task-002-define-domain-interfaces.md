# Task 002: Define domain layer interfaces

**depends-on**: task-001

## Description

Define the core domain interfaces that form the architectural foundation. These interfaces define the contracts between layers and enable dependency injection for testability.

## Execution Context

**Task Number**: 2 of 16
**Phase**: Foundation
**Prerequisites**: Project structure created

## Domain Interfaces to Define

### 1. DiffFilter Interface
```go
// domain/diff/filter.go
type DiffFilter interface {
    Filter(ctx context.Context, diff *StagedDiff) (*StagedDiff, error)
}
```

### 2. DiffTruncator Interface
```go
// domain/diff/truncator.go
type DiffTruncator interface {
    Truncate(ctx context.Context, diff *StagedDiff, maxLines int) (*StagedDiff, bool, error)
}
```

### 3. CommitMessageGenerator Interface
```go
// domain/commit/generator.go
type CommitMessageGenerator interface {
    Generate(ctx context.Context, req GenerateRequest) (*CommitMessage, error)
}
```

### 4. HookExecutor Interface
```go
// domain/hook/executor.go
type HookExecutor interface {
    Execute(ctx context.Context, hookPath string, input HookInput) (*HookResult, error)
}
```

### 5. ScopeGenerator Interface (for ga init)
```go
// domain/project/generator.go
type ScopeGenerator interface {
    Generate(ctx context.Context, req ScopeRequest) (*ProjectConfig, error)
}
```

## Value Objects to Define

### StagedDiff
- Files: []string
- Content: string
- Lines: int

### CommitMessage
- Title: string
- Body: string
- Outline: string

### ProjectConfig
- Scopes: []string

### HookInput (JSON schema)
- Diff: string
- CommitMessage: string
- Intent: string
- StagedFiles: []string
- Config: ProjectConfig

### HookResult
- ExitCode: int
- Stderr: string

## Steps

### Step 1: Create domain value objects
Create files in `domain/*/` directories with struct definitions

### Step 2: Create domain interfaces
Define interfaces in respective `domain/*/interfaces.go` files

### Step 3: Add empty implementations (stubs)
Create minimal struct implementations that satisfy interfaces (for compilation)

## Files to Create/Modify

- Create: `domain/diff/filter.go`
- Create: `domain/diff/truncator.go`
- Create: `domain/diff/staged.go` (StagedDiff)
- Create: `domain/commit/generator.go`
- Create: `domain/commit/message.go`
- Create: `domain/hook/executor.go`
- Create: `domain/hook/input.go`
- Create: `domain/hook/result.go`
- Create: `domain/project/generator.go`
- Create: `domain/project/config.go`

## Verification Commands

```bash
go build -o git-agent .
```

## Success Criteria

- All interfaces defined with proper signatures
- Code compiles without errors
- Interfaces are documented with godoc comments
