# Task 001: Project Setup and Dependencies

**depends-on**: (none)

## Description

Set up the directory structure and dependencies for the graph feature. This creates the skeleton that all subsequent tasks build upon. No build tags needed -- SQLite via `modernc.org/sqlite` is pure Go, so graph code compiles in the default binary.

## Execution Context

**Task Number**: 001 of 020
**Phase**: Setup
**Prerequisites**: None

## BDD Scenario

```gherkin
Scenario: Graph dependencies compile in default build
  Given the git-agent-cli project exists
  When I run "make build"
  Then the binary should compile as pure Go
  And the binary should include graph commands
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (Architecture section)

## Files to Modify/Create

- Create: `domain/graph/` directory
- Create: `infrastructure/graph/` directory
- Create: `infrastructure/treesitter/` directory
- Create: `pkg/graph/` directory
- Modify: `go.mod` (add `modernc.org/sqlite` and `gotreesitter` dependencies)

## Steps

### Step 1: Create directory structure

Create the following empty directories with placeholder files:
- `domain/graph/`
- `infrastructure/graph/`
- `infrastructure/treesitter/`
- `pkg/graph/`

### Step 2: Add dependencies

```bash
go get modernc.org/sqlite@latest
go get github.com/nicois/gotreesitter@v0.6.4
```

### Step 3: Verify default build is unaffected

- **Verification**: `make build` succeeds as pure Go
- **Verification**: `make test` passes (all existing tests)

## Verification Commands

```bash
# Default build still works
make build

# Existing tests pass
make test
```

## Success Criteria

- Directory structure created
- `make build` succeeds as pure Go (no regression)
- `make test` passes
