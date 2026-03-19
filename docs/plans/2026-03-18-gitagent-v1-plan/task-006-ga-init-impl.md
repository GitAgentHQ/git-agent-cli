# Task 006: ga init command implementation

**depends-on**: task-005

## Description

Implement the `ga init` command that generates .ga/project.yml from git history and top-level directories using an LLM.

## Execution Context

**Task Number**: 6 of 16
**Phase**: Core Features
**Prerequisites**: Init tests written

## Implementation Components

### 1. cmd/init.go
- Flag definitions: --force, --hook, --max-commits
- Parse flags into InitInput DTO
- Call InitService.Execute()

### 2. application/init_service.go
- Orchestrate the init flow:
  1. Validate git repository
  2. Read git log (git log --format="%s" --max-count=N)
  3. Scan top-level directories
  4. Build LLM prompt with subjects + dirs
  5. Call LLM, receive scopes + reasoning
  6. Write .ga/project.yml
  7. Install hook (or empty placeholder)

### 3. infrastructure/openai/prompt_builder.go
- Build init prompt with:
  - Commit subjects
  - Directory names
  - Request for scopes array

### 4. infrastructure/fs/config_writer.go
- Write .ga/project.yml with scopes list

### 5. infrastructure/fs/hook_installer.go
- Create .ga/hooks/pre-commit
- chmod +x
- Install from embed or empty placeholder

### 6. Embedded hooks
- hooks/empty.sh: #!/bin/sh, exit 0
- hooks/conventional.sh: validates conventional commit format

## Files to Modify/Create

- Create: `cmd/init.go`
- Create: `application/init_service.go`
- Create: `application/dto/init_input.go`
- Modify: `hooks/empty.sh` (embedded)
- Modify: `hooks/conventional.sh` (embedded)
- Modify: `main.go` (add init command)

## Steps

### Step 1: Create embed directives
- Add //go:embed for hooks directory

### Step 2: Implement InitInput DTO
- MaxCommits: int
- Force: bool
- Hook: string

### Step 3: Implement InitService
- Orchestrate init flow
- Handle all error cases

### Step 4: Implement cmd/init.go
- Parse flags
- Handle output (stdout = config, stderr = reasoning)

### Step 5: Register command in main.go
- Add init command to cobra

### Step 6: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./cmd/... -v -run TestInit
go test ./application/... -v -run TestInit
```

## Success Criteria

- All init tests pass
- Command works end-to-end with mock LLM
- Correct file outputs and permissions
- Proper error messages for all error scenarios
