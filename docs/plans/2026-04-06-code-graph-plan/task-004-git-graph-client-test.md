# Task 004: Git graph client extensions test (RED)

**depends-on**: task-002

## Description
Write failing tests for the git client extensions needed by graph indexing. These methods wrap git CLI commands (CommitLogDetailed, CurrentHead, MergeBaseIsAncestor, HashObject, DiffNameOnly, DiffForFiles).

## Execution Context
**Task Number**: 004 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-002 (domain types defined, GraphGitClient interface exists)
**Parallel with**: task-003 (SQLite lifecycle)

## BDD Scenario
```gherkin
Feature: Git graph client extensions

  Scenario: CommitLogDetailed returns structured commit data
    Given a git repository with 3 commits modifying different files
    When I call CommitLogDetailed with no sinceHash
    Then I receive 3 CommitInfo structs
    And each has Hash, Message, AuthorName, AuthorEmail, Timestamp
    And each has a Files slice with Status and Path

  Scenario: CommitLogDetailed handles renames
    Given a git repository where file "old.go" was renamed to "new.go"
    When I call CommitLogDetailed
    Then the rename commit has a FileChange with Status "R", OldPath "old.go", Path "new.go"

  Scenario: CommitLogDetailed with sinceHash returns only new commits
    Given a git repository with 5 commits
    And I know the hash of commit 3
    When I call CommitLogDetailed with sinceHash=commit3
    Then I receive exactly 2 CommitInfo structs (commits 4 and 5)

  Scenario: CurrentHead returns HEAD hash
    Given a git repository with at least one commit
    When I call CurrentHead
    Then I receive a 40-character hex string matching git rev-parse HEAD

  Scenario: MergeBaseIsAncestor returns true for ancestor
    Given a git repository with commits A -> B -> C
    When I call MergeBaseIsAncestor(A, C)
    Then it returns true

  Scenario: MergeBaseIsAncestor returns false for non-ancestor
    Given a git repository with diverged branches
    When I call MergeBaseIsAncestor(branchA_head, branchB_head)
    Then it returns false

  Scenario: HashObject returns blob hash
    Given a git repository with a file "test.go"
    When I call HashObject("test.go")
    Then I receive a 40-character hex string

  Scenario: HashObject returns deleted sentinel for missing file
    Given a git repository
    When I call HashObject("nonexistent.go")
    Then I receive "deleted"

  Scenario: DiffNameOnly returns changed files
    Given a git repository with staged and unstaged changes
    When I call DiffNameOnly
    Then I receive a sorted list of changed file paths

  Scenario: DiffForFiles returns diff output
    Given a git repository with modifications to "a.go" and "b.go"
    When I call DiffForFiles(["a.go", "b.go"])
    Then I receive combined diff text for both files
```

## Files to Modify/Create
- `infrastructure/git/graph_client_test.go` -- all test functions

## Steps
### Step 1: Write test helpers
Create helper functions to set up temporary git repositories with commits, renames, and file modifications.

### Step 2: Write test functions
- `TestGraphClient_CommitLogDetailed`
- `TestGraphClient_CommitLogDetailed_Renames`
- `TestGraphClient_CommitLogDetailed_SinceHash`
- `TestGraphClient_CurrentHead`
- `TestGraphClient_MergeBaseIsAncestor`
- `TestGraphClient_MergeBaseIsAncestor_False`
- `TestGraphClient_HashObject`
- `TestGraphClient_HashObject_Deleted`
- `TestGraphClient_DiffNameOnly`
- `TestGraphClient_DiffForFiles`

### Step 3: Verify tests fail
```bash
go test ./infrastructure/git/... -run TestGraphClient -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./infrastructure/git/... -run TestGraphClient -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles (`go vet ./infrastructure/git/...`)
- All tests FAIL (Red phase)
- Tests cover all GraphGitClient interface methods
- Each test creates its own temporary git repository (no shared state)
