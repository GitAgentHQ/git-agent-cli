# Task 004: Git Graph Client Extensions Impl

**depends-on**: task-004-git-graph-client-test

## Description

Implement the GraphGitClient interface methods by extending the existing `infrastructure/git/client.go` with detailed commit log parsing, file content retrieval at specific commits, current HEAD, and diff operations.

## Execution Context

**Task Number**: 004 of 020 (impl)
**Phase**: Infrastructure Foundation
**Prerequisites**: Failing tests from task-004-git-graph-client-test

## BDD Scenario

```gherkin
Scenario: First-time full index of a git repository
  Given the repository has 3 commits modifying 5 files
  When I run "git-agent graph index"
  Then the graph should contain 3 Commit nodes
  And the graph should contain 5 File nodes
  And the graph should contain Author nodes for each unique committer
  And the graph should contain MODIFIES edges linking commits to files
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing)

## Files to Modify/Create

- Create: `infrastructure/git/graph_client.go` -- new file with graph-specific methods
- Modify: `infrastructure/git/client.go` -- if needed to expose shared helpers

## Steps

### Step 1: Implement CommitLogDetailed

Parse `git log --format=...` with `--name-status` to extract commit hash, message, author name/email, timestamp, parent hashes, and per-file modifications (status + path). Support `since` hash filter and `max` limit.

### Step 2: Implement FileContentAt

Run `git show {hash}:{path}` to retrieve file content at a specific commit. Return error if file does not exist at that commit.

### Step 3: Implement CurrentHead

Run `git rev-parse HEAD` to get the current HEAD commit hash.

### Step 4: Implement Diff and DiffFiles

- `Diff`: Combine `git diff` (unstaged) and `git diff --cached` (staged) output
- `DiffFiles`: Parse `git diff --name-only` + `git diff --cached --name-only` for changed file paths

### Step 5: Verify tests pass (Green)

- **Verification**: `go test ./infrastructure/git/... -run TestGraphGitClient` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./infrastructure/git/... -run TestGraphGitClient -v

# Existing git tests unaffected
go test ./infrastructure/git/... -v
```

## Success Criteria

- All GraphGitClient interface methods implemented
- All graph git client tests pass (Green)
- Existing git client tests still pass (no regression)
