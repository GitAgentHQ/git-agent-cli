# Task 003: SQLite Client Lifecycle Test

**depends-on**: task-002

## Description

Write tests for SQLite client lifecycle operations: opening a database, initializing the schema, closing the connection, and dropping the database. Tests run against a real embedded SQLite instance in a temporary directory.

## Execution Context

**Task Number**: 003 of 020 (test)
**Phase**: Infrastructure Foundation
**Prerequisites**: Domain interfaces from task-002

## BDD Scenario

```gherkin
Scenario: SQLite client creates database on Open
  Given no database file exists at the target path
  When I call Open on the SQLiteClient
  Then a database file should be created at the target path
  And the connection should be usable for queries

Scenario: SQLite client initializes schema
  Given an open SQLite connection
  When I call InitSchema
  Then all node tables (Commit, File, Symbol, Author, Session, Action, IndexState) should exist
  And all edge tables (AUTHORED, MODIFIES, CONTAINS, CALLS, IMPORTS, CO_CHANGED, SESSION_CONTAINS, ACTION_MODIFIES, ACTION_PRODUCES) should exist

Scenario: SQLite client drops database
  Given an existing graph database at the target path
  When I call Drop on the SQLiteClient
  Then the database file should be removed from disk

Scenario: SQLite client reconnects to existing database
  Given a database was previously created and closed
  When I call Open on the same path
  Then the connection should succeed
  And previously stored data should be accessible
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (SQLite client lifecycle)

## Files to Modify/Create

- Create: `infrastructure/graph/sqlite_client_test.go`

## Steps

### Step 1: Create test file

Create `infrastructure/graph/sqlite_client_test.go`.

### Step 2: Write lifecycle tests

Test cases:
- `TestSQLiteClient_OpenCreatesDatabase`: Open on non-existent path creates the database file
- `TestSQLiteClient_InitSchema`: After open, InitSchema creates all node and edge tables
- `TestSQLiteClient_Close`: Close releases resources, subsequent operations fail
- `TestSQLiteClient_Drop`: Drop removes the database file from disk
- `TestSQLiteClient_OpenExisting`: Open on existing database reconnects without data loss

### Step 3: Verify tests fail (Red)

- **Verification**: `go test ./infrastructure/graph/... -run TestSQLiteClient` -- tests MUST FAIL (no implementation yet)

## Verification Commands

```bash
# Tests should fail (Red)
go test ./infrastructure/graph/... -run TestSQLiteClient -v
```

## Success Criteria

- Test file created
- Tests cover all lifecycle operations
- Tests use temporary directories for isolation
- All tests FAIL (Red phase)
