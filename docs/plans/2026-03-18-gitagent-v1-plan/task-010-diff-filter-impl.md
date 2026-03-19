# Task 010: Diff filter implementation

**depends-on**: task-009

## Description

Implement diff filtering and truncation for excluding lock files, binary files, and limiting diff size.

## Execution Context

**Task Number**: 10 of 16
**Phase**: Core Features
**Prerequisites**: Diff filter tests written

## Implementation Components

### 1. pkg/filter/patterns.go
Define filter patterns for:
- Lock files: package-lock.json, yarn.lock, pnpm-lock.yaml, go.sum, Gemfile.lock, Pipfile.lock, Cargo.lock, etc.
- Binary files: *.png, *.jpg, *.jpeg, *.gif, *.ico, *.pdf, *.zip, *.tar.gz, etc.
- Generated files: dist/, build/, *.min.js, *.css

### 2. domain/diff/filter.go
Implement DiffFilter interface:
- Parse diff headers to identify files
- Filter out lock files by pattern
- Filter out binary files (check for binary indicators in diff)
- Return filtered StagedDiff

### 3. domain/diff/truncator.go
Implement DiffTruncator interface:
- Count lines in diff content
- If > maxLines, truncate
- Return truncated diff + wasTruncated flag
- Include truncation info in metadata

### 4. infrastructure/git/diff_provider.go
- Implement git diff --staged extraction
- Parse file list from diff headers

## Files to Modify/Create

- Modify: `pkg/filter/patterns.go`
- Create: `pkg/filter/truncator.go`
- Create: `infrastructure/git/diff_provider.go`
- Create: `infrastructure/diff/filter.go`
- Create: `infrastructure/diff/truncator.go`

## Steps

### Step 1: Implement filter patterns
- Define lock file patterns
- Define binary file patterns
- Define function to match patterns

### Step 2: Implement DiffFilter
- Parse diff to identify files
- Filter based on patterns
- Return filtered result

### Step 3: Implement DiffTruncator
- Count lines
- Truncate if needed
- Return truncation metadata

### Step 4: Implement diff provider
- Extract staged diff via git
- Parse into StagedDiff struct

### Step 5: Run tests
- Verify all tests pass

## Verification Commands

```bash
go test ./domain/diff/... -v
go test ./pkg/filter/... -v
go test ./infrastructure/... -v
```

## Success Criteria

- All diff filter tests pass
- All diff truncator tests pass
- Correct handling of edge cases
- Proper warning output for truncation
