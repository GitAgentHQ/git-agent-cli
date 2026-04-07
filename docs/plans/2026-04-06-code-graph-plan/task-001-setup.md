# Task 001: Project Setup and Build Tags

**depends-on**: (none)

## Description

Set up the directory structure, build tags, and Makefile targets for the graph feature. This creates the skeleton that all subsequent tasks build upon.

## Execution Context

**Task Number**: 001 of 020
**Phase**: Setup
**Prerequisites**: None

## BDD Scenario

```gherkin
Scenario: Build tag isolation preserves default build
  Given the git-agent-cli project exists
  When I run "make build"
  Then the binary should compile without CGo
  And the binary should not include graph commands
  When I run "make build-graph"
  Then the binary should compile with CGo and the "graph" build tag
  And the binary should include graph commands
```

**Spec Source**: `../2026-04-02-code-graph-design/architecture.md` (Build Tag Isolation section)

## Files to Modify/Create

- Create: `domain/graph/` directory
- Create: `infrastructure/graph/` directory
- Create: `infrastructure/treesitter/` directory
- Create: `pkg/graph/` directory
- Modify: `Makefile` (add `build-graph`, `test-graph` targets)
- Modify: `go.mod` (add `go-kuzu` and `gotreesitter` dependencies)

## Steps

### Step 1: Create directory structure

Create the following empty directories with placeholder files:
- `domain/graph/`
- `infrastructure/graph/`
- `infrastructure/treesitter/`
- `pkg/graph/`

### Step 2: Add Makefile targets

Add these targets to the existing `Makefile`:
- `build-graph`: `go build -tags graph -o git-agent .`
- `test-graph`: `go test -tags graph -count=1 ./...`
- `install-graph`: `go install -tags graph .`

### Step 3: Add dependencies

```bash
go get github.com/kuzudb/go-kuzu@v0.11.3
go get github.com/nicois/gotreesitter@v0.6.4
```

### Step 4: Verify default build is unaffected

- **Verification**: `make build` succeeds without CGo
- **Verification**: `make test` passes (all existing tests)

## Verification Commands

```bash
# Default build still works
make build

# Graph build compiles
make build-graph

# Existing tests pass
make test
```

## Success Criteria

- Directory structure created
- Makefile has graph-specific targets
- `make build` succeeds without CGo (no regression)
- `make build-graph` compiles with CGo
- `make test` passes
