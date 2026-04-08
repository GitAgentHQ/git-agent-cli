# Task 008: Impact query service implementation (GREEN)

**depends-on**: task-008-test

## Description
Implement the impact query service. Queries co_changed table for a given file, resolves renames through the rename chain, ranks results by coupling_strength, and applies depth/top-N/min_count filters.

## Execution Context
**Task Number**: 008 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-008-test (failing tests exist)

## BDD Scenario
```gherkin
Feature: Impact query implementation

  Scenario: All impact query tests pass
    Given the ImpactService is implemented
    When I run the impact query tests
    Then all tests pass (Green phase)
```

## Files to Modify/Create
- `application/graph_impact_service.go` -- ImpactService struct

## Steps
### Step 1: Define ImpactService struct
```go
type ImpactService struct {
    repo graph.GraphRepository
}
```

### Step 2: Implement Query method
```go
func (s *ImpactService) Query(ctx context.Context, req graph.ImpactRequest) (*graph.ImpactResult, error)
```

Logic:
1. Resolve the target file through rename chain (follow renames to canonical path)
2. Query co_changed WHERE file_a = target OR file_b = target
3. Apply min_count filter
4. If depth > 1: recursively query co-changed for each result (BFS, visited set to prevent cycles)
5. Sort by coupling_strength descending
6. Apply top-N limit
7. Include rename chain in result

### Step 3: Implement rename resolution
Walk the renames table to build a chain: old -> intermediate -> current.
When querying co_changed, check both current and all historical names.

### Step 4: Run tests
```bash
go test ./application/... -run TestImpactQuery -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestImpactQuery -v
# All tests must PASS
make build
make test
```

## Success Criteria
- All `TestImpactQuery_*` tests pass
- Results ranked by coupling_strength descending
- Rename resolution follows full chain
- Depth parameter controls transitive expansion (BFS)
- min_count and top-N filters applied correctly
- Empty result (no error) for files with no co-changes
- `make build` and `make test` pass
