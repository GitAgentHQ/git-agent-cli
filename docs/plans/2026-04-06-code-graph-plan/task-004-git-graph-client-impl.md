# Task 004: Git graph client extensions implementation (GREEN)

**depends-on**: task-004-test

## Description
Implement the git client extensions for graph indexing. The `GraphClient` struct wraps git CLI subprocess calls, following the same pattern as the existing `infrastructure/git/client.go`.

## Execution Context
**Task Number**: 004 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-004-test (failing tests exist)
**Note**: `infrastructure/git/graph_client.go` already exists from prior work -- verify and update as needed

## BDD Scenario
```gherkin
Feature: Git graph client implementation

  Scenario: All git graph client tests pass
    Given the GraphClient is implemented
    When I run the git graph client tests
    Then all tests pass (Green phase)

  Scenario: GraphClient satisfies GraphGitClient interface
    Given the GraphClient struct
    Then it implements all methods of graph.GraphGitClient
    And a compile-time assertion var _ graph.GraphGitClient = (*GraphClient)(nil) exists
```

## Files to Modify/Create
- `infrastructure/git/graph_client.go` -- GraphClient struct (already exists, verify completeness)

## Steps
### Step 1: Verify existing implementation
The file `infrastructure/git/graph_client.go` already exists with implementations for:
- CommitLogDetailed (with parseCommitLog, parseRawLine, parseNumStatLine helpers)
- CurrentHead
- MergeBaseIsAncestor
- HashObject
- DiffNameOnly
- DiffForFiles

### Step 2: Verify compile-time interface check
Ensure `var _ graph.GraphGitClient = (*GraphClient)(nil)` is present.

### Step 3: Fix any failing tests
Update implementation if any test cases reveal issues.

### Step 4: Run tests
```bash
go test ./infrastructure/git/... -run TestGraphClient -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./infrastructure/git/... -run TestGraphClient -v
# All tests must PASS
go vet ./infrastructure/git/...
make build
make test
```

## Success Criteria
- All `TestGraphClient_*` tests pass
- Compile-time interface assertion present
- `make build` and `make test` pass
- No Tree-sitter references in the implementation
