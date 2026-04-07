# Task 004: Git Graph Client Extensions Test

**depends-on**: task-002

## Description

Write tests for the extended git client methods needed by the graph feature: detailed commit log parsing, file content retrieval at a specific commit, current HEAD hash, and diff operations. Tests run against a real temporary git repository.

## Execution Context

**Task Number**: 004 of 020 (test)
**Phase**: Infrastructure Foundation
**Prerequisites**: Domain interfaces from task-002 (GraphGitClient interface)

## BDD Scenario

```gherkin
Scenario: CommitLogDetailed returns structured commit data
  Given a temporary git repository with 3 commits modifying 5 files
  When I call CommitLogDetailed with no since filter
  Then it should return 3 commit entries
  And each entry should have hash, message, author name/email, timestamp, parent hashes
  And each entry should include changed files with status (A/M/D)

Scenario: CommitLogDetailed respects since filter
  Given a repository with 5 commits
  When I call CommitLogDetailed with since set to the 3rd commit hash
  Then only the 2 commits after that hash should be returned

Scenario: FileContentAt returns file content at a specific commit
  Given a repository where "main.go" was modified across commits
  When I call FileContentAt with a specific commit hash and "main.go"
  Then it should return the file content as it existed at that commit

Scenario: Diff returns unstaged and staged changes
  Given a repository with uncommitted modifications to "src/main.go"
  When I call Diff
  Then it should return the unified diff of all changes
  When I call DiffFiles
  Then it should return ["src/main.go"]
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (GraphGitClient interface)

## Files to Modify/Create

- Create: `infrastructure/git/graph_client_test.go`

## Steps

### Step 1: Create test file

Create `infrastructure/git/graph_client_test.go` with test helpers to create temporary git repositories with deterministic commits.

### Step 2: Write CommitLogDetailed tests

- `TestGraphGitClient_CommitLogDetailed`: Returns commits with hash, message, author, timestamp, parent hashes, and changed files (with status A/M/D)
- `TestGraphGitClient_CommitLogDetailedSince`: Only returns commits after a given hash
- `TestGraphGitClient_CommitLogDetailedMaxCommits`: Respects the max limit

### Step 3: Write FileContentAt tests

- `TestGraphGitClient_FileContentAt`: Returns file content at a specific commit hash
- `TestGraphGitClient_FileContentAt_DeletedFile`: Returns error for deleted files

### Step 4: Write CurrentHead and Diff tests

- `TestGraphGitClient_CurrentHead`: Returns the current HEAD commit hash
- `TestGraphGitClient_Diff`: Returns combined unstaged + staged diff
- `TestGraphGitClient_DiffFiles`: Returns list of changed file paths

### Step 5: Verify tests fail (Red)

- **Verification**: `go test ./infrastructure/git/... -run TestGraphGitClient` -- tests MUST FAIL

## Verification Commands

```bash
# Tests should fail (Red)
go test ./infrastructure/git/... -run TestGraphGitClient -v
```

## Success Criteria

- Test file created with proper test helpers
- Tests cover all GraphGitClient interface methods
- Tests use temporary git repositories with deterministic setup
- All tests FAIL (Red phase)
