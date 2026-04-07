# Task 003: KuzuDB Client Lifecycle Impl

**depends-on**: task-003-kuzu-lifecycle-test

## Description

Implement KuzuDB client with lifecycle operations: open database, initialize schema with all node/relationship tables from the design, close connection, and drop database. This is the foundation for all graph persistence.

## Execution Context

**Task Number**: 003 of 020 (impl)
**Phase**: Infrastructure Foundation
**Prerequisites**: Failing tests from task-003-kuzu-lifecycle-test

## BDD Scenario

```gherkin
Scenario: KuzuDB client creates database on Open
  Given no database directory exists at the target path
  When I call Open on the KuzuClient
  Then a database directory should be created at the target path

Scenario: KuzuDB client initializes schema
  Given an open KuzuDB connection
  When I call InitSchema
  Then all node and relationship tables should exist

Scenario: KuzuDB client drops database
  Given an existing graph database
  When I call Drop
  Then the database directory should be removed from disk
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (KuzuDB client lifecycle)

## Files to Modify/Create

- Create: `infrastructure/graph/kuzu_client.go` (with `//go:build graph` tag)
- Create: `infrastructure/graph/kuzu_repository.go` (with `//go:build graph` tag) -- skeleton implementing GraphRepository interface, with lifecycle methods implemented and query/write methods as stubs

## Steps

### Step 1: Implement KuzuDB client struct

Create `KuzuClient` struct wrapping the `go-kuzu` database connection. Constructor takes a path string (`.git-agent/graph.db`).

### Step 2: Implement Open

Open the KuzuDB database at the given path. Configure buffer pool to 256MB and thread cap to 4. Create the directory if it does not exist.

### Step 3: Implement InitSchema

Execute all CREATE NODE TABLE IF NOT EXISTS and CREATE REL TABLE IF NOT EXISTS statements from the design's DDL (Commit, File, Symbol, Author, Session, Action, IndexState + all relationship tables).

### Step 4: Implement Close and Drop

Close releases the database connection. Drop closes and removes the database directory from disk.

### Step 5: Verify tests pass (Green)

- **Verification**: `go test -tags graph ./infrastructure/graph/... -run TestKuzuClient` -- all tests PASS

## Verification Commands

```bash
# Tests should pass (Green)
go test -tags graph ./infrastructure/graph/... -run TestKuzuClient -v

# Existing tests unaffected
make test
```

## Success Criteria

- KuzuDB client opens, initializes schema, closes, and drops correctly
- All lifecycle tests pass (Green)
- `make test` still passes (no regression from build tags)
