# Task 007: Co-change computation implementation (GREEN)

**depends-on**: task-007-test

## Description
Implement co-change computation using SQL self-join on the modifies table. Full recompute clears and rebuilds co_changed. Incremental mode recomputes only pairs involving files from newly indexed commits.

## Execution Context
**Task Number**: 007 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-007-test (failing tests exist)

## BDD Scenario
```gherkin
Feature: Co-change computation implementation

  Scenario: All co-change tests pass
    Given the co-change computation is implemented
    When I run the co-change tests
    Then all tests pass (Green phase)
```

## Files to Modify/Create
- `infrastructure/graph/cochange.go` -- SQL queries for co-change computation
- Application service wiring in `application/graph_index_service.go` or separate service

## Steps
### Step 1: Implement full recompute SQL
```sql
-- Self-join on modifies to find file pairs in the same commit
INSERT OR REPLACE INTO co_changed (file_a, file_b, co_count, commits_a, commits_b, coupling_strength)
SELECT
  m1.file_path AS file_a,
  m2.file_path AS file_b,
  COUNT(DISTINCT m1.commit_hash) AS co_count,
  (SELECT COUNT(DISTINCT commit_hash) FROM modifies WHERE file_path = m1.file_path) AS commits_a,
  (SELECT COUNT(DISTINCT commit_hash) FROM modifies WHERE file_path = m2.file_path) AS commits_b,
  CAST(COUNT(DISTINCT m1.commit_hash) AS REAL) /
    MAX(
      (SELECT COUNT(DISTINCT commit_hash) FROM modifies WHERE file_path = m1.file_path),
      (SELECT COUNT(DISTINCT commit_hash) FROM modifies WHERE file_path = m2.file_path)
    ) AS coupling_strength
FROM modifies m1
JOIN modifies m2 ON m1.commit_hash = m2.commit_hash AND m1.file_path < m2.file_path
GROUP BY m1.file_path, m2.file_path;
```

### Step 2: Implement incremental recompute
For a set of affected files (from new commits):
1. Delete existing co_changed rows where file_a OR file_b is in the affected set
2. Recompute only pairs involving at least one affected file
3. Apply same coupling_strength formula

### Step 3: Implement min_count filtering
Query method accepts min_count parameter and filters `WHERE co_count >= ?`.

### Step 4: Wire into repository
Add `RecomputeCoChanged(ctx, affectedFiles []string)` and `RecomputeCoChangedFull(ctx)` to the SQLite repository.

### Step 5: Run tests
```bash
go test ./application/... -run TestCoChange -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCoChange -v
# All tests must PASS
make build
make test
```

## Success Criteria
- All `TestCoChange_*` tests pass
- Full recompute SQL self-join produces correct pairs
- Incremental mode only recomputes affected pairs
- Coupling strength = co_count / max(commits_a, commits_b)
- Canonical ordering (file_a < file_b) enforced
- No self-pairs (a.go, a.go)
- `make build` and `make test` pass
