# Task 006: EnsureIndex middleware implementation (GREEN)

**depends-on**: task-006-test

## Description
Implement the EnsureIndex middleware that auto-indexes the graph database. Decision logic: DB missing -> full index; unreachable lastHash -> full re-index; HEAD == lastHash -> skip; else -> incremental.

## Execution Context
**Task Number**: 006 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-006-test (failing tests exist)

## BDD Scenario
```gherkin
Feature: EnsureIndex implementation

  Scenario: All EnsureIndex tests pass
    Given the EnsureIndexService is implemented
    When I run the EnsureIndex tests
    Then all tests pass (Green phase)
```

## Files to Modify/Create
- `application/graph_ensure_index_service.go` -- EnsureIndexService struct

## Steps
### Step 1: Define EnsureIndexService struct
```go
type EnsureIndexService struct {
    indexService *IndexService
    repo         graph.GraphRepository
    git          graph.GraphGitClient
}
```

### Step 2: Implement EnsureIndex method
```
func (s *EnsureIndexService) EnsureIndex(ctx context.Context, req graph.IndexRequest) (*graph.IndexResult, error)
```

Logic:
1. Check if graph.db exists at `req.RepoPath/.git-agent/graph.db`
2. If `req.ForceReindex` or DB does not exist:
   - Drop existing DB if present
   - Open + InitSchema
   - Run full index
   - Return result
3. Open DB, get `last_indexed_commit` from index_state
4. Get current HEAD
5. If HEAD == lastHash: return (0 commits, not incremental)
6. Check `git.MergeBaseIsAncestor(lastHash, HEAD)`
7. If not ancestor (force-push recovery):
   - Drop + recreate
   - Full index
8. If ancestor:
   - Incremental index (sinceHash=lastHash)
9. Update last_indexed_commit
10. Return result

### Step 3: Handle progress reporting
Accept an `io.Writer` for progress output (written to stderr in CLI context).

### Step 4: Run tests
```bash
go test ./application/... -run TestEnsureIndex -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestEnsureIndex -v
# All tests must PASS
make build
make test
```

## Success Criteria
- All `TestEnsureIndex_*` tests pass
- Decision tree handles all 5 scenarios correctly
- Force-push recovery (unreachable hash) triggers full re-index
- Already up-to-date returns 0 commits indexed
- `make build` and `make test` pass
