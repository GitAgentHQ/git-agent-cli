# Task 019: P1 CLI Wiring

**depends-on**: task-014, task-015, task-016, task-018

## Description

Wire all P1 graph subcommands into Cobra: `graph capture`, `graph timeline`, `graph hotspots`, `graph ownership`. Update `graph index` to support the `--ast` flag. Each command constructs services with wired dependencies.

## Execution Context

**Task Number**: 019 of 020
**Phase**: Integration (P1)
**Prerequisites**: All P1 service implementations (tasks 012-018)

## BDD Scenario

```gherkin
Scenario: Capture action via CLI
  When I run "git-agent graph capture --source claude-code --tool Edit"
  Then the output should be valid JSON with action_id, session_id

Scenario: Timeline query via CLI
  When I run "git-agent graph timeline --since 2h"
  Then the output should be valid JSON with sessions array

Scenario: Hotspots query via CLI
  When I run "git-agent graph hotspots --top 10"
  Then the output should list files ranked by change frequency

Scenario: Ownership query via CLI
  When I run "git-agent graph ownership pkg/service.go"
  Then the output should list authors ranked by commit count

Scenario: Index with AST flag via CLI
  When I run "git-agent graph index --ast"
  Then symbols should be extracted and stored in the graph
```

**Spec Source**: `../2026-04-02-code-graph-design/bdd-specs.md` (all P1 features)

## Files to Modify/Create

- Create: `cmd/graph_capture.go` -- `graph capture` subcommand (with `//go:build graph` tag)
- Create: `cmd/graph_timeline.go` -- `graph timeline` subcommand
- Create: `cmd/graph_hotspots.go` -- `graph hotspots` subcommand
- Create: `cmd/graph_ownership.go` -- `graph ownership` subcommand
- Modify: `cmd/graph.go` -- register new subcommands
- Modify: `cmd/graph_index.go` -- add `--ast` flag

## Steps

### Step 1: Wire capture command

Parse `--source` (required), `--tool`, `--session`, `--message`, `--end-session` flags. Construct CaptureService. Call `svc.Capture(ctx, req)` or `svc.EndSession(ctx, id)`. Output CaptureResult as JSON.

### Step 2: Wire timeline command

Parse `--since`, `--until`, `--source`, `--file`, `--compress`, `--top` flags. For `--compress`, also construct LLM client. Call `svc.Timeline(ctx, req)`. Output TimelineResult as JSON.

### Step 3: Wire hotspots command

Parse `--path`, `--top`, `--since`, `--exclude-tests`, `--exclude-generated` flags. Call `svc.Hotspots(ctx, req)`. Output HotspotsResult as JSON.

### Step 4: Wire ownership command

Parse positional `PATH` argument and `--since` flag. Call `svc.Ownership(ctx, req)`. Output OwnershipResult as JSON.

### Step 5: Add --ast flag to index

Add `--ast` boolean flag to `graph index`. Pass through to IndexRequest.AST. When set, construct ASTParser and pass to GraphService.

### Step 6: Verify commands compile and show help

- **Verification**: `make build-graph` succeeds
- **Verification**: All new commands show proper help text

## Verification Commands

```bash
# Build with graph support
make build-graph

# Verify CLI help for all P1 commands
./git-agent graph capture --help
./git-agent graph timeline --help
./git-agent graph hotspots --help
./git-agent graph ownership --help
./git-agent graph index --help | grep -q "ast"
```

## Success Criteria

- All P1 subcommands registered and accessible
- Flags match the design specification
- `--ast` flag added to `graph index`
- `make build-graph` compiles successfully
