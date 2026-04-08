# Task 001: Setup -- add SQLite dep, create directories

**depends-on**: none

## Description
Add the `modernc.org/sqlite` pure-Go SQLite dependency and create the directory structure for the graph feature. No Tree-sitter dependency -- co-change analysis uses git history only.

## Execution Context
**Task Number**: 001 of 018
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: None (first task)

## BDD Scenario
```gherkin
Feature: Project setup for code graph

  Scenario: SQLite dependency is available
    Given the go.mod file
    When I check for modernc.org/sqlite
    Then it appears as a direct dependency
    And no tree-sitter dependency exists

  Scenario: Directory structure exists
    Given the repository
    When I list domain/graph/
    Then the directory exists
    When I list infrastructure/graph/
    Then the directory exists
    And infrastructure/treesitter/ does NOT exist

  Scenario: Build and tests pass
    Given the new dependency is added
    When I run make build
    Then the binary compiles successfully
    When I run make test
    Then all existing tests pass
```

## Files to Modify/Create
- `go.mod` -- add `modernc.org/sqlite` via `go get`
- `domain/graph/` -- create directory (already exists from prior work)
- `infrastructure/graph/` -- create directory (already exists from prior work)

## Steps
### Step 1: Add SQLite dependency
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go get modernc.org/sqlite
```

### Step 2: Verify directory structure
Confirm `domain/graph/` and `infrastructure/graph/` directories exist. Create if missing.

### Step 3: Verify no Tree-sitter dependency
```bash
grep -r treesitter go.mod go.sum || echo "No tree-sitter -- correct"
```

### Step 4: Build and test
```bash
make build
make test
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
grep 'modernc.org/sqlite' go.mod           # dependency present
grep -r treesitter go.mod && exit 1 || true # no tree-sitter
ls domain/graph/                            # directory exists
ls infrastructure/graph/                    # directory exists
make build                                  # compiles
make test                                   # all tests pass
```

## Success Criteria
- `modernc.org/sqlite` appears in `go.mod`
- No tree-sitter dependency in `go.mod` or `go.sum`
- `domain/graph/` directory exists
- `infrastructure/graph/` directory exists
- `infrastructure/treesitter/` does NOT exist
- `make build` succeeds
- `make test` passes all existing tests
