# Task 001: Setup project structure and dependencies

**depends-on**: N/A (first task)

## Description

Create the initial Go project structure with dependencies and folder layout. This establishes the foundation for the entire CLI application.

## Execution Context

**Task Number**: 1 of 16
**Phase**: Setup
**Prerequisites**: None

## Steps

### Step 1: Initialize Go module
- Create `go.mod` with module name `github.com/fradser/git-agent`
- Set Go version to 1.21

### Step 2: Add dependencies
- Add `github.com/spf13/cobra v1.8.0`
- Add `github.com/sashabaranov/go-openai v1.24.0`

### Step 3: Create folder structure
Create the following directories:
```
ga-cli/
├── main.go
├── hooks/                           # built-in hook templates (embedded)
│   ├── empty.sh
│   └── conventional.sh
├── cmd/
├── domain/
│   ├── commit/
│   ├── diff/
│   ├── hook/
│   └── project/
├── application/
│   └── dto/
├── infrastructure/
│   ├── git/
│   ├── openai/
│   ├── hook/
│   ├── fs/
│   └── config/
└── pkg/
    ├── errors/
    └── filter/
```

### Step 4: Create main.go
- Simple entry point that calls `cobra.Execute()`

### Step 5: Create root command
- Create `cmd/root.go` with basic cobra setup
- Add global flags (--verbose, etc.)

### Step 6: Verify
- Run `go mod tidy`
- Run `go build -o git-agent .` to verify compilation

## Verification Commands

```bash
go mod init github.com/fradser/git-agent
go get github.com/spf13/cobra@v1.8.0
go get github.com/sashabaranov/go-openai@v1.24.0
go mod tidy
go build -o git-agent .
./ga --help
```

## Success Criteria

- Project compiles without errors
- `ga --help` shows the CLI help
- No placeholder code warnings
