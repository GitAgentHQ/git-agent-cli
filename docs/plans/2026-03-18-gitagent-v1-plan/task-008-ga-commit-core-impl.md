# Task 008: git agent commit core flow implementation

**depends-on**: task-007

## Description

Implement the core `git agent commit` flow including LLM integration, message assembly, and git commit execution.

## Execution Context

**Task Number**: 8 of 16
**Phase**: Core Features
**Prerequisites**: Commit tests written

## Implementation Components

### 1. cmd/commit.go
- Flag definitions: --api-key, --model, --base-url, --intent, --co-author, --max-diff-lines, --dry-run, --verbose
- Parse flags into CommitInput DTO
- Call CommitService.Execute()

### 2. application/commit_service.go
- Orchestrate commit flow:
  1. Validate staged changes exist
  2. Extract staged diff (git diff --staged)
  3. Filter diff (lock files, binaries)
  4. Truncate diff if needed
  5. Build LLM prompt
  6. Call LLM
  7. Validate LLM response
  8. Assemble commit message
  9. Execute hook (if exists)
  10. Execute git commit
  11. Output to stdout/stderr

### 3. infrastructure/openai/client.go
- Call OpenAI-compatible API
- Handle response parsing
- Handle errors

### 4. infrastructure/openai/prompt_builder.go
- Build commit prompt:
  - Include scopes from config if present
  - Include intent if provided
  - Request structured JSON response

### 5. infrastructure/git/committer.go
- Execute `git commit -m "..."`

### 6. application/dto/commit_input.go
- All flag values and resolved config

## Files to Modify/Create

- Create: `cmd/commit.go`
- Create: `application/commit_service.go`
- Create: `application/dto/commit_input.go`
- Modify: `main.go` (add commit command)

## Steps

### Step 1: Implement CommitInput DTO
- All flag values
- Resolved config values

### Step 2: Implement CommitService
- Implement orchestration flow
- Use interfaces for dependencies

### Step 3: Implement OpenAI client wrapper
- Wrap go-openai client
- Use OpenAI-compatible client for all endpoints

### Step 4: Implement prompt builder
- Build commit prompt with scopes, intent

### Step 5: Implement git committer
- Execute git commit subprocess

### Step 6: Implement cmd/commit.go
- Parse flags
- Handle output separation

### Step 7: Register command in main.go

### Step 8: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./cmd/... -v -run TestCommit
go test ./application/... -v -run TestCommit
```

## Success Criteria

- All commit tests pass
- Command works end-to-end with mock LLM
- Proper message assembly
- Correct stdout/stderr handling
