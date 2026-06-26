# Code Knowledge Graph Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Load `superpowers:executing-plans` skill using the Skill tool to implement this plan task-by-task.

**Goal:** Build an invisible graph infrastructure that auto-indexes git history into SQLite and enables co-change-based impact analysis, commit enhancement, action capture, and timeline display -- all without requiring users to learn new concepts.

**Architecture:** Clean Architecture with strict inward dependency flow (`cmd -> application -> domain <- infrastructure`). SQLite lives exclusively in `infrastructure/`. Always-on in a single binary -- no build tags, no CGo.

**Tech Stack:** Go 1.26, SQLite via `modernc.org/sqlite` (pure Go, zero CGo), Cobra CLI, existing OpenAI-compatible client for LLM features.

**Design Support:**
- [BDD Specs](../2026-04-02-code-graph-design/bdd-specs.md)
- [Architecture](../2026-04-02-code-graph-design/architecture.md)
- [Requirements](../2026-04-02-code-graph-design/requirements.md)
- [Best Practices](../2026-04-02-code-graph-design/best-practices.md)

## Context

Coding agents lack structural awareness of codebases. When an agent needs to answer "what will break if I change this?", today's options (git log, grep, reading entire codebase) are inadequate.

The original design placed 12 subcommands under `git-agent graph`. After review, the design was radically simplified:

- **Insight**: The graph should be invisible infrastructure (like `.git/index`), not 12 CLI commands
- **Outcome**: 3 visible commands + 1 hidden plumbing + 1 invisible commit enhancement
- **Approach**: SQLite only, no Tree-sitter. Binary +8-12MB (not +28MB)

This is greenfield work -- no existing graph code exists in the codebase.

## Key Design Decisions

1. **No `graph` namespace** -- `impact`/`timeline`/`diagnose`/`capture` register on rootCmd as flat top-level commands
2. **No Tree-sitter** -- co-change analysis only needs git history. Binary +8-12MB not +28MB
3. **No `index`/`status`/`reset` commands** -- auto-index via EnsureIndex, `--verbose` for status, `rm .git-agent/graph.db*` for reset
4. **TTY-aware output** -- text for terminal, JSON for pipe, `--json`/`--text` override
5. **Commit enhancement is invisible** -- zero new flags, zero new concepts for users. Co-change hints injected into planning prompt
6. **Capture always exits 0** -- never blocks agent hooks
7. **Delta-based capture** -- `capture_baseline` prevents diff accumulation across tool calls
8. **Session instance_id** -- `$PPID` distinguishes concurrent agents
9. **Incremental co-change** -- recompute only affected pairs, full on --force or >500 commits
10. **Force-push recovery** -- `merge-base --is-ancestor`, fallback to full re-index

## Execution Plan

<!-- Dependency convention: bare "NNN" means both test and impl for that number are complete.
     "NNN-test" means only the test task. Impl tasks always depend on their paired test. -->
```yaml
tasks:
  # Phase P0: Co-change + Impact + Commit Enhancement
  - id: "001"
    subject: "Setup -- add SQLite dep, create directories"
    slug: "setup"
    type: "setup"
    depends-on: []
  - id: "002"
    subject: "Domain types and interfaces (zero external imports)"
    slug: "domain-types"
    type: "setup"
    depends-on: ["001"]
  - id: "003"
    subject: "SQLite client lifecycle test"
    slug: "sqlite-lifecycle-test"
    type: "test"
    depends-on: ["002"]
  - id: "003"
    subject: "SQLite client lifecycle impl"
    slug: "sqlite-lifecycle-impl"
    type: "impl"
    depends-on: ["003-test"]
  - id: "004"
    subject: "Git graph client extensions test"
    slug: "git-graph-client-test"
    type: "test"
    depends-on: ["002"]
  - id: "004"
    subject: "Git graph client extensions impl"
    slug: "git-graph-client-impl"
    type: "impl"
    depends-on: ["004-test"]
  - id: "005"
    subject: "Full graph index test"
    slug: "index-full-test"
    type: "test"
    depends-on: ["003", "004"]
  - id: "005"
    subject: "Full graph index impl"
    slug: "index-full-impl"
    type: "impl"
    depends-on: ["005-test"]
  - id: "006"
    subject: "EnsureIndex + incremental test"
    slug: "ensure-index-test"
    type: "test"
    depends-on: ["005"]
  - id: "006"
    subject: "EnsureIndex + incremental impl"
    slug: "ensure-index-impl"
    type: "impl"
    depends-on: ["006-test"]
  - id: "007"
    subject: "CO_CHANGED computation test"
    slug: "co-change-test"
    type: "test"
    depends-on: ["005"]
  - id: "007"
    subject: "CO_CHANGED computation impl"
    slug: "co-change-impl"
    type: "impl"
    depends-on: ["007-test"]
  - id: "008"
    subject: "Impact query test"
    slug: "impact-query-test"
    type: "test"
    depends-on: ["007"]
  - id: "008"
    subject: "Impact query impl"
    slug: "impact-query-impl"
    type: "impl"
    depends-on: ["008-test"]
  - id: "009"
    subject: "impact CLI command"
    slug: "impact-cli"
    type: "impl"
    depends-on: ["006", "008"]
  - id: "010"
    subject: "Commit co-change enhancement"
    slug: "commit-enhance"
    type: "impl"
    depends-on: ["006", "007"]
  - id: "011"
    subject: "P0 E2E test"
    slug: "e2e-p0-test"
    type: "test"
    depends-on: ["009", "010"]
  - id: "011"
    subject: "P0 E2E impl"
    slug: "e2e-p0-impl"
    type: "impl"
    depends-on: ["011-test"]

  # Phase P1b: Action Tracking Pipeline
  - id: "012"
    subject: "Capture service test"
    slug: "capture-test"
    type: "test"
    depends-on: ["003", "004"]
  - id: "012"
    subject: "Capture service impl"
    slug: "capture-impl"
    type: "impl"
    depends-on: ["012-test"]
  - id: "013"
    subject: "capture CLI command"
    slug: "capture-cli"
    type: "impl"
    depends-on: ["012"]
  - id: "014"
    subject: "Timeline service test"
    slug: "timeline-test"
    type: "test"
    depends-on: ["012"]
  - id: "014"
    subject: "Timeline service impl"
    slug: "timeline-impl"
    type: "impl"
    depends-on: ["014-test"]
  - id: "015"
    subject: "timeline CLI command"
    slug: "timeline-cli"
    type: "impl"
    depends-on: ["014"]
  - id: "016"
    subject: "diagnose stub command"
    slug: "diagnose-stub"
    type: "impl"
    depends-on: []
  - id: "017"
    subject: "Action-to-commit linking"
    slug: "action-commit-linking"
    type: "impl"
    depends-on: ["012", "010"]
  - id: "018"
    subject: "P1b E2E test"
    slug: "e2e-p1b-test"
    type: "test"
    depends-on: ["013", "015", "016"]
  - id: "018"
    subject: "P1b E2E impl"
    slug: "e2e-p1b-impl"
    type: "impl"
    depends-on: ["018-test"]
```

**Task File References (for detailed BDD scenarios):**
- [Task 001: Setup](./task-001-setup.md)
- [Task 002: Domain types](./task-002-domain-types.md)
- [Task 003: SQLite client lifecycle test](./task-003-sqlite-lifecycle-test.md)
- [Task 003: SQLite client lifecycle impl](./task-003-sqlite-lifecycle-impl.md)
- [Task 004: Git graph client extensions test](./task-004-git-graph-client-test.md)
- [Task 004: Git graph client extensions impl](./task-004-git-graph-client-impl.md)
- [Task 005: Full graph index test](./task-005-index-full-test.md)
- [Task 005: Full graph index impl](./task-005-index-full-impl.md)
- [Task 006: EnsureIndex + incremental test](./task-006-ensure-index-test.md)
- [Task 006: EnsureIndex + incremental impl](./task-006-ensure-index-impl.md)
- [Task 007: CO_CHANGED computation test](./task-007-co-change-test.md)
- [Task 007: CO_CHANGED computation impl](./task-007-co-change-impl.md)
- [Task 008: Impact query test](./task-008-impact-query-test.md)
- [Task 008: Impact query impl](./task-008-impact-query-impl.md)
- [Task 009: impact CLI command](./task-009-impact-cli.md)
- [Task 010: Commit co-change enhancement](./task-010-commit-enhance.md)
- [Task 011: P0 E2E test](./task-011-e2e-p0-test.md)
- [Task 011: P0 E2E impl](./task-011-e2e-p0-impl.md)
- [Task 012: Capture service test](./task-012-capture-test.md)
- [Task 012: Capture service impl](./task-012-capture-impl.md)
- [Task 013: capture CLI command](./task-013-capture-cli.md)
- [Task 014: Timeline service test](./task-014-timeline-test.md)
- [Task 014: Timeline service impl](./task-014-timeline-impl.md)
- [Task 015: timeline CLI command](./task-015-timeline-cli.md)
- [Task 016: diagnose stub](./task-016-diagnose-stub.md)
- [Task 017: Action-to-commit linking](./task-017-action-commit-linking.md)
- [Task 018: P1b E2E test](./task-018-e2e-p1b-test.md)
- [Task 018: P1b E2E impl](./task-018-e2e-p1b-impl.md)

## BDD Coverage

All non-P2 BDD scenarios from the design are covered across the task files:

| Feature | Scenarios | Tasks |
|---------|-----------|-------|
| Graph Indexing (auto) | 8 | 005, 006, 007 |
| Impact Query | 5 | 008, 009 |
| Action Capture | 9 | 012, 013 |
| Timeline | 5 (excl. P2) | 014, 015 |
| Commit Enhancement | 3 | 010 |
| EnsureIndex | 4 | 006 |

P2 scenarios excluded: All Diagnose scenarios (4), Timeline compression fails gracefully, Stability, Co-change clusters.

## Dependency Chain

```
001 (setup)
 |
 v
002 (domain types)
 |
 +----------+-----------+
 |          |           |
 v          v           v
003        004         016 (diagnose stub, independent)
(sqlite)   (git)
 |          |
 +----+-----+
      |
      v
     005 (index full)
      |
 +----+-----+
 |          |
 v          v
006        007
(ensure)   (co-change)
 |          |
 |    +-----+
 |    |
 v    v
 +----+--> 008 (impact query)
 |         |
 |         v
 +-------> 009 (impact CLI)
 |
 +-------> 010 (commit enhance) <-- 007
 |
 v
011 (e2e p0) <-- 009, 010

003+004 --> 012 (capture)
             |
             +--> 013 (capture CLI)
             |
             +--> 014 (timeline) --> 015 (timeline CLI)
             |
             +--> 017 (action-commit) <-- 010

013+015+016 --> 018 (e2e p1b)
```

**Analysis:**
- No circular dependencies
- P0 path: 001 -> 002 -> 003/004 -> 005 -> 007 -> 008 -> 009 -> 011
- P1b branches from 003+004 (capture), then timeline, then linking
- Maximum parallel width: 3 tasks (003, 004, 016 after 002)
- Critical path length: 9 (001 -> 002 -> 003+004 -> 005 -> 007 -> 008 -> 009 -> 011)

## Files Modified (Existing)

| File | Change |
|------|--------|
| `domain/commit/planner.go` | Add `CoChangeHints []CoChangeHint` to PlanRequest |
| `application/commit_service.go` | Add optional CoChangeProvider, query before Plan(), link actions after Commit() |
| `infrastructure/openai/client.go` | Append co-change section to plan prompt (line ~417) |
| `infrastructure/git/client.go` | (read-only -- pattern reference for graph_client.go) |
| `pkg/errors/errors.go` | Add `ErrGraphNotIndexed` exit code 3 |
| `go.mod` | Add `modernc.org/sqlite` |

## Files Created (New)

| File | Purpose |
|------|---------|
| `domain/graph/types.go` | CommitNode, FileNode, AuthorNode, CoChangedEntry, RenameEntry |
| `domain/graph/session.go` | SessionNode, ActionNode, CaptureRequest, CaptureResult, CaptureBaseline |
| `domain/graph/query.go` | ImpactRequest, ImpactResult, TimelineRequest, TimelineResult |
| `domain/graph/repository.go` | GraphRepository interface (lifecycle + write + read + capture + baseline) |
| `domain/graph/git_client.go` | GraphGitClient interface |
| `domain/graph/index.go` | IndexRequest, IndexResult |
| `infrastructure/graph/sqlite_client.go` | SQLite connection + schema DDL (13 tables) |
| `infrastructure/graph/sqlite_repository.go` | GraphRepository implementation |
| `infrastructure/graph/cochange.go` | Co-change SQL queries |
| `infrastructure/git/graph_client.go` | New git methods for indexing |
| `application/graph_index_service.go` | Full + incremental indexing |
| `application/graph_ensure_index_service.go` | EnsureIndex middleware |
| `application/graph_impact_service.go` | Impact query orchestration |
| `application/capture_service.go` | Delta-based action capture |
| `application/timeline_service.go` | Timeline query |
| `cmd/impact.go` | `git-agent impact` command |
| `cmd/capture.go` | `git-agent capture` command (hidden) |
| `cmd/timeline.go` | `git-agent timeline` command |
| `cmd/diagnose.go` | `git-agent diagnose` stub |

## Verification

```bash
# After P0:
make test                                    # all existing + new tests pass
make build && ./git-agent impact --help      # impact command exists
./git-agent impact application/commit_service.go  # returns co-changed files
./git-agent impact application/commit_service.go --json | jq .  # valid JSON
./git-agent commit                           # commit works, planning silently enhanced

# After P1b:
./git-agent capture --source test --tool Edit  # exits 0
./git-agent timeline                           # shows captured actions
./git-agent diagnose "test"                    # prints "not yet implemented"
```

## Comparison

| Metric | Original Design | This Plan |
|--------|----------------|-----------|
| Namespaces | 1 (`graph`) | **0** |
| Visible commands | 12 | **3** |
| Hidden commands | 0 | 1 (`capture`) |
| New dependencies | SQLite + Tree-sitter | **SQLite only** |
| Binary increase | +18-28MB | **+8-12MB** |
| Schema tables | 18 | **13** |
| Implementation tasks | 20 | **18** |
| AST parsing | Yes (5 languages) | **No** |

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-04-06-code-graph-plan/`. Execution options:

**1. Orchestrated Execution (Recommended)** - Load `superpowers:executing-plans` skill using the Skill tool.

**2. Direct Agent Team** - Load `superpowers:agent-team-driven-development` skill using the Skill tool.

**3. BDD-Focused Execution** - Load `superpowers:behavior-driven-development` skill using the Skill tool for specific scenarios.
