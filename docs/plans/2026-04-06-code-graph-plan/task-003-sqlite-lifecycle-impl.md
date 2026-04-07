# Task 003: SQLite Client Lifecycle Impl

**depends-on**: task-003-sqlite-lifecycle-test

## Description

Implement SQLite client with lifecycle operations: open database, initialize schema with all tables from the design, close connection, and drop database. This is the foundation for all graph persistence.

## Execution Context

**Task Number**: 003 of 020 (impl)
**Phase**: Infrastructure Foundation
**Prerequisites**: Failing tests from task-003-sqlite-lifecycle-test

## BDD Scenario

```gherkin
Scenario: SQLite client creates database on Open
  Given no database file exists at the target path
  When I call Open on the SQLiteClient
  Then a database file should be created at the target path

Scenario: SQLite client initializes schema
  Given an open SQLite connection
  When I call InitSchema
  Then all node and edge tables should exist

Scenario: SQLite client drops database
  Given an existing graph database
  When I call Drop
  Then the database file should be removed from disk
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (SQLite client lifecycle)

## Files to Modify/Create

- Create: `infrastructure/graph/sqlite_client.go`
- Create: `infrastructure/graph/sqlite_repository.go` -- skeleton implementing GraphRepository interface, with lifecycle methods implemented and query/write methods as stubs

## Steps

### Step 1: Implement SQLite client struct

Create `SQLiteClient` struct wrapping the `modernc.org/sqlite` database connection. Constructor takes a path string (`.git-agent/graph.db`).

### Step 2: Implement Open

Open the SQLite database at the given path. Configure WAL mode, busy_timeout=5000, synchronous=NORMAL, cache_size=-64000. Create the parent directory if it does not exist.

### Step 3: Implement InitSchema

Execute all CREATE TABLE IF NOT EXISTS statements from the design's DDL (Commit, File, Symbol, Author, Session, Action, IndexState + all edge tables).

### Step 4: Implement Close and Drop

Close releases the database connection. Drop closes and removes the database file from disk.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test ./infrastructure/graph/... -run TestSQLiteClient` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test ./infrastructure/graph/... -run TestSQLiteClient -v

# Existing tests unaffected
make test
```

## Success Criteria

- SQLite client opens, initializes schema, closes, and drops correctly
- All lifecycle tests pass (Green)
- `make test` still passes (no regression)
