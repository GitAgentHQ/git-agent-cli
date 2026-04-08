# Task 003: SQLite client lifecycle implementation (GREEN)

**depends-on**: task-003-test

## Description
Implement the SQLite client that satisfies the lifecycle tests. Uses `modernc.org/sqlite` (pure Go, no CGo). Sets performance PRAGMAs and creates all 13 tables with CREATE TABLE IF NOT EXISTS.

## Execution Context
**Task Number**: 003 of 018 (impl phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-003-test (failing tests exist)

## BDD Scenario
```gherkin
Feature: SQLite client implementation

  Scenario: All lifecycle tests pass
    Given the SQLite client is implemented
    When I run the lifecycle tests
    Then all tests pass (Green phase)
```

## Files to Modify/Create
- `infrastructure/graph/sqlite_client.go` -- SQLiteClient struct with Open, Close, InitSchema, Drop

## Steps
### Step 1: Implement SQLiteClient struct
```go
type SQLiteClient struct {
    db     *sql.DB
    dbPath string
}
```

### Step 2: Implement Open with PRAGMAs
Open the database and set:
- `PRAGMA journal_mode=WAL`
- `PRAGMA busy_timeout=5000`
- `PRAGMA synchronous=NORMAL`
- `PRAGMA cache_size=-64000`

### Step 3: Implement InitSchema with 13 tables
CREATE TABLE IF NOT EXISTS for all 13 tables:
1. `commits` (hash PK, message, timestamp)
2. `files` (path PK, total_commits)
3. `authors` (email PK, name)
4. `modifies` (commit_hash, file_path, status, old_path, additions, deletions)
5. `authored` (commit_hash, author_email)
6. `co_changed` (file_a, file_b, co_count, commits_a, commits_b, coupling_strength)
7. `renames` (old_path, new_path, commit_hash)
8. `index_state` (key PK, value)
9. `sessions` (id PK, instance_id, started_at, source)
10. `actions` (id PK, session_id, tool, timestamp, intent)
11. `action_modifies` (action_id, file_path, before_hash, after_hash)
12. `action_produces` (action_id, file_path, after_hash)
13. `capture_baseline` (session_id, file_path, hash)

### Step 4: Implement Close and Drop

### Step 5: Run tests
```bash
go test ./infrastructure/graph/... -run TestSQLiteClient -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./infrastructure/graph/... -run TestSQLiteClient -v
# All tests must PASS
make build
make test
```

## Success Criteria
- All `TestSQLiteClient_*` tests pass
- WAL mode is active
- Exactly 13 tables created (no symbols/contains_symbol/calls/imports)
- PRAGMAs set to specified values
- `make build` and `make test` pass
