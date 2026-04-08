# Task 003: SQLite client lifecycle test (RED)

**depends-on**: task-002

## Description
Write failing tests for the SQLite client lifecycle: Open, Close, InitSchema, Drop. Verify WAL mode and that all 13 tables are created (NOT 18 -- no symbols, contains_symbol, calls, imports tables).

## Execution Context
**Task Number**: 003 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-002 (domain types defined)
**Parallel with**: task-004 (git graph client test)

## BDD Scenario
```gherkin
Feature: SQLite client lifecycle

  Scenario: Open creates database file in WAL mode
    Given no graph.db file exists
    When I call Open with a temp directory path
    Then a graph.db file is created
    And the journal_mode PRAGMA returns "wal"

  Scenario: InitSchema creates all 13 tables
    Given an open SQLite connection
    When I call InitSchema
    Then exactly 13 tables exist: commits, files, authors, modifies, authored,
         co_changed, renames, index_state, sessions, actions,
         action_modifies, action_produces, capture_baseline
    And no symbols table exists
    And no contains_symbol table exists
    And no calls table exists
    And no imports table exists

  Scenario: InitSchema is idempotent
    Given an open SQLite connection with schema initialized
    When I call InitSchema again
    Then no error occurs
    And all 13 tables still exist

  Scenario: Close releases the database
    Given an open SQLite connection
    When I call Close
    Then subsequent queries return an error

  Scenario: Drop removes the database file
    Given an open SQLite connection
    When I call Drop
    Then the graph.db file no longer exists

  Scenario: PRAGMAs are set correctly
    Given an open SQLite connection
    When I query PRAGMA busy_timeout
    Then it returns 5000
    When I query PRAGMA synchronous
    Then it returns 1 (NORMAL)
    When I query PRAGMA cache_size
    Then it returns -64000
```

## Files to Modify/Create
- `infrastructure/graph/sqlite_client_test.go` -- all test functions

## Steps
### Step 1: Write test file
Create `infrastructure/graph/sqlite_client_test.go` with test functions:
- `TestSQLiteClient_Open_CreatesFile`
- `TestSQLiteClient_Open_WALMode`
- `TestSQLiteClient_InitSchema_Creates13Tables`
- `TestSQLiteClient_InitSchema_NoSymbolsTables`
- `TestSQLiteClient_InitSchema_Idempotent`
- `TestSQLiteClient_Close`
- `TestSQLiteClient_Drop`
- `TestSQLiteClient_Pragmas`

### Step 2: Verify tests fail
```bash
go test ./infrastructure/graph/... -run TestSQLiteClient -v
```
Tests must fail (Red phase of BDD-driven TDD).

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./infrastructure/graph/... -run TestSQLiteClient -v 2>&1 | grep FAIL
# Tests MUST fail -- this is the Red phase
```

## Success Criteria
- Test file compiles (`go vet ./infrastructure/graph/...`)
- All tests FAIL (Red phase)
- Tests cover: Open, WAL mode, 13 tables, no symbol tables, idempotent schema, Close, Drop, PRAGMAs
- No test uses Tree-sitter or references symbol tables
