# Task 010: P0 CLI Wiring

**depends-on**: task-008, task-009

## Description

Wire all P0 graph subcommands into Cobra: `graph` parent command, `graph index`, `graph blast-radius`, `graph status`, and `graph reset`. Each command constructs services with wired dependencies, parses flags, and outputs JSON to stdout.

## Execution Context

**Task Number**: 010 of 020
**Phase**: Integration (P0)
**Prerequisites**: GraphService with Index (task-005), Lifecycle (task-008), and BlastRadius (task-009)

## BDD Scenario

```gherkin
Scenario: First-time full index via CLI
  Given a git repository with commits
  When I run "git-agent graph index"
  Then the command should output JSON with indexed_commits, new_commits, files, authors, duration_ms
  And the command should exit with code 0

Scenario: Blast radius query via CLI
  Given an indexed repository
  When I run "git-agent graph blast-radius pkg/service.go"
  Then the output should be valid JSON with target, co_changed, importers, callers fields

Scenario: Status query via CLI
  Given an indexed repository
  When I run "git-agent graph status"
  Then the output should be valid JSON with exists, node_counts, edge_counts

Scenario: Reset via CLI
  Given an indexed repository
  When I run "git-agent graph reset"
  Then the output should contain {"deleted": true}
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (Graph Indexing, Blast Radius, Graph Lifecycle)

## Files to Modify/Create

- Create: `cmd/graph.go` -- parent `graph` command (with `//go:build graph` tag)
- Create: `cmd/graph_index.go` -- `graph index` subcommand
- Create: `cmd/graph_blast_radius.go` -- `graph blast-radius` subcommand
- Create: `cmd/graph_status.go` -- `graph status` subcommand
- Create: `cmd/graph_reset.go` -- `graph reset` subcommand
- Create: `pkg/graph/format.go` -- JSON output formatting helpers

## Steps

### Step 1: Create parent graph command

Create `cmd/graph.go` with `//go:build graph` tag. Register subcommands and add to rootCmd in `init()`. Add shared `--format` and `--verbose` flags.

### Step 2: Wire graph index command

Parse `--max-commits`, `--force`, `--ast`, `--max-files-per-commit` flags. Construct KuzuRepository, GraphGitClient, and GraphService. Call `svc.Index(ctx, req)`. Output IndexResult as JSON. Handle gitignore integration.

### Step 3: Wire blast-radius command

Parse positional `PATH` argument plus `--symbol`, `--depth`, `--top`, `--min-count` flags. Call `svc.BlastRadius(ctx, req)`. Output BlastRadiusResult as JSON.

### Step 4: Wire status command

Call `svc.Status(ctx)`. Output GraphStatus as JSON. Exit with code 3 if graph does not exist.

### Step 5: Wire reset command

Call `svc.Reset(ctx)`. Output deletion confirmation as JSON.

### Step 6: Create JSON formatting helpers

Create `pkg/graph/format.go` with helpers to marshal graph results to JSON, consistent with the output format specification (snake_case keys, RFC 3339 timestamps, _ms durations).

### Step 7: Verify commands compile

- **Verification**: `make build-graph` succeeds and `./git-agent graph --help` shows subcommands

## Verification Commands

```bash
# Build with graph support
make build-graph

# Verify CLI help
./git-agent graph --help
./git-agent graph index --help
./git-agent graph blast-radius --help
./git-agent graph status --help
./git-agent graph reset --help

# Default build unaffected
make build
```

## Success Criteria

- All P0 graph subcommands registered and accessible
- Flags match the design specification
- JSON output follows the format specification
- Build tag isolation works (default build has no graph commands)
- `make build-graph` compiles successfully
