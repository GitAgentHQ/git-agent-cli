# Task 003: KuzuDB Client Lifecycle Test

**depends-on**: task-002

## Description

Write tests for KuzuDB client lifecycle operations: opening a database, initializing the schema, closing the connection, and dropping the database. Tests run against a real embedded KuzuDB instance in a temporary directory.

## Execution Context

**Task Number**: 003 of 020 (test)
**Phase**: Infrastructure Foundation
**Prerequisites**: Domain interfaces from task-002

## BDD Scenario

```gherkin
Scenario: KuzuDB client creates database on Open
  Given no database directory exists at the target path
  When I call Open on the KuzuClient
  Then a database directory should be created at the target path
  And the connection should be usable for queries

Scenario: KuzuDB client initializes schema
  Given an open KuzuDB connection
  When I call InitSchema
  Then all node tables (Commit, File, Symbol, Author, Session, Action, IndexState) should exist
  And all relationship tables (AUTHORED, MODIFIES, CONTAINS, CALLS, IMPORTS, CO_CHANGED, SESSION_CONTAINS, ACTION_MODIFIES, ACTION_PRODUCES) should exist

Scenario: KuzuDB client drops database
  Given an existing graph database at the target path
  When I call Drop on the KuzuClient
  Then the database directory should be removed from disk

Scenario: KuzuDB client reconnects to existing database
  Given a database was previously created and closed
  When I call Open on the same path
  Then the connection should succeed
  And previously stored data should be accessible
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (KuzuDB client lifecycle)

## Files to Modify/Create

- Create: `infrastructure/graph/kuzu_client_test.go` (with `//go:build graph` tag)

## Steps

### Step 1: Create test file with build tag

Create `infrastructure/graph/kuzu_client_test.go` with `//go:build graph` tag.

### Step 2: Write lifecycle tests

Test cases:
- `TestKuzuClient_OpenCreatesDatabase`: Open on non-existent path creates the database directory
- `TestKuzuClient_InitSchema`: After open, InitSchema creates all node and relationship tables
- `TestKuzuClient_Close`: Close releases resources, subsequent operations fail
- `TestKuzuClient_Drop`: Drop removes the database directory from disk
- `TestKuzuClient_OpenExisting`: Open on existing database reconnects without data loss

### Step 3: Verify tests fail (Red)

- **Verification**: `go test -tags graph ./infrastructure/graph/... -run TestKuzuClient` -- tests MUST FAIL (no implementation yet)

## Verification Commands

```bash
# Tests should fail (Red)
go test -tags graph ./infrastructure/graph/... -run TestKuzuClient -v
```

## Success Criteria

- Test file created with proper build tag
- Tests cover all lifecycle operations
- Tests use temporary directories for isolation
- All tests FAIL (Red phase)
