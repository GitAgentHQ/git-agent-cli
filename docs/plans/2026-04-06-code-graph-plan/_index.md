# Code Knowledge Graph Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Load `superpowers:executing-plans` skill using the Skill tool to implement this plan task-by-task.

**Goal:** Build a graph-based code intelligence engine (`git-agent graph`) that indexes git history, AST structures, and agent/human actions into SQLite, enabling blast-radius analysis, hotspot detection, ownership queries, action capture, and timeline display.

**Architecture:** Clean Architecture with strict inward dependency flow (`cmd -> application -> domain <- infrastructure`). SQLite and Tree-sitter live exclusively in `infrastructure/`. Always-on in a single binary -- no build tags, no CGo.

**Tech Stack:** Go 1.26, SQLite via `modernc.org/sqlite` (pure Go, zero CGo), `gotreesitter` v0.6.4 (pure Go), Cobra CLI, existing OpenAI-compatible client for LLM features.

**Design Support:**
- [BDD Specs](../2026-04-02-code-graph-design/bdd-specs.md)
- [Architecture](../2026-04-02-code-graph-design/architecture.md)
- [Requirements](../2026-04-02-code-graph-design/requirements.md)
- [Research](../2026-04-02-code-graph-design/research.md)
- [Best Practices](../2026-04-02-code-graph-design/best-practices.md)

## Context

Coding agents lack structural awareness of codebases. When an agent needs to answer "what will break if I change this function?", today's options (git log, grep, reading entire codebase) are inadequate. Beyond structural awareness, agents lack behavioral traceability -- agent edits collapse into git commits, losing fine-grained action history.

A pre-computed graph database over git history + AST structure + agent action timeline enables O(1) relationship lookups and behavioral tracing via CLI subcommands returning machine-parseable JSON.

This is greenfield work -- no existing graph code exists in the codebase.

## Execution Plan

<!-- Dependency convention: bare "NNN" means both test and impl for that number are complete.
     "NNN-test" means only the test task. Impl tasks always depend on their paired test. -->
```yaml
tasks:
  - id: "001"
    subject: "Project setup and dependencies"
    slug: "setup"
    type: "setup"
    depends-on: []
  - id: "002"
    subject: "Domain types and interfaces"
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
    subject: "Incremental index test"
    slug: "index-incremental-test"
    type: "test"
    depends-on: ["005"]
  - id: "006"
    subject: "Incremental index impl"
    slug: "index-incremental-impl"
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
    subject: "Graph lifecycle test"
    slug: "lifecycle-test"
    type: "test"
    depends-on: ["003"]
  - id: "008"
    subject: "Graph lifecycle impl"
    slug: "lifecycle-impl"
    type: "impl"
    depends-on: ["008-test"]
  - id: "009"
    subject: "File-level blast radius test"
    slug: "blast-radius-file-test"
    type: "test"
    depends-on: ["005", "007"]
  - id: "009"
    subject: "File-level blast radius impl"
    slug: "blast-radius-file-impl"
    type: "impl"
    depends-on: ["009-test"]
  - id: "010"
    subject: "P0 CLI wiring"
    slug: "cli-p0"
    type: "impl"
    depends-on: ["008", "009"]
  - id: "011"
    subject: "P0 E2E tests"
    slug: "e2e-p0-test"
    type: "test"
    depends-on: ["010"]
  - id: "011"
    subject: "P0 E2E impl"
    slug: "e2e-p0-impl"
    type: "impl"
    depends-on: ["011-test"]
  - id: "012"
    subject: "Tree-sitter parser test"
    slug: "treesitter-test"
    type: "test"
    depends-on: ["002"]
  - id: "012"
    subject: "Tree-sitter parser impl"
    slug: "treesitter-impl"
    type: "impl"
    depends-on: ["012-test"]
  - id: "013"
    subject: "AST indexing test"
    slug: "ast-index-test"
    type: "test"
    depends-on: ["005", "012"]
  - id: "013"
    subject: "AST indexing impl"
    slug: "ast-index-impl"
    type: "impl"
    depends-on: ["013-test"]
  - id: "014"
    subject: "Symbol-level blast radius test"
    slug: "blast-radius-symbol-test"
    type: "test"
    depends-on: ["009", "013"]
  - id: "014"
    subject: "Symbol-level blast radius impl"
    slug: "blast-radius-symbol-impl"
    type: "impl"
    depends-on: ["014-test"]
  - id: "015"
    subject: "Hotspots query test"
    slug: "hotspots-test"
    type: "test"
    depends-on: ["005"]
  - id: "015"
    subject: "Hotspots query impl"
    slug: "hotspots-impl"
    type: "impl"
    depends-on: ["015-test"]
  - id: "016"
    subject: "Ownership query test"
    slug: "ownership-test"
    type: "test"
    depends-on: ["005"]
  - id: "016"
    subject: "Ownership query impl"
    slug: "ownership-impl"
    type: "impl"
    depends-on: ["016-test"]
  - id: "017"
    subject: "Action capture test"
    slug: "capture-test"
    type: "test"
    depends-on: ["003", "004"]
  - id: "017"
    subject: "Action capture impl"
    slug: "capture-impl"
    type: "impl"
    depends-on: ["017-test"]
  - id: "018"
    subject: "Timeline query test"
    slug: "timeline-test"
    type: "test"
    depends-on: ["017"]
  - id: "018"
    subject: "Timeline query impl"
    slug: "timeline-impl"
    type: "impl"
    depends-on: ["018-test"]
  - id: "019"
    subject: "P1 CLI wiring"
    slug: "cli-p1"
    type: "impl"
    depends-on: ["014", "015", "016", "018"]
  - id: "020"
    subject: "P1 E2E tests"
    slug: "e2e-p1-test"
    type: "test"
    depends-on: ["019"]
  - id: "020"
    subject: "P1 E2E impl"
    slug: "e2e-p1-impl"
    type: "impl"
    depends-on: ["020-test"]
```

**Task File References (for detailed BDD scenarios):**
- [Task 001: Project setup and dependencies](./task-001-setup.md)
- [Task 002: Domain types and interfaces](./task-002-domain-types.md)
- [Task 003: SQLite client lifecycle test](./task-003-sqlite-lifecycle-test.md)
- [Task 003: SQLite client lifecycle impl](./task-003-sqlite-lifecycle-impl.md)
- [Task 004: Git graph client extensions test](./task-004-git-graph-client-test.md)
- [Task 004: Git graph client extensions impl](./task-004-git-graph-client-impl.md)
- [Task 005: Full graph index test](./task-005-index-full-test.md)
- [Task 005: Full graph index impl](./task-005-index-full-impl.md)
- [Task 006: Incremental index test](./task-006-index-incremental-test.md)
- [Task 006: Incremental index impl](./task-006-index-incremental-impl.md)
- [Task 007: CO_CHANGED computation test](./task-007-co-change-test.md)
- [Task 007: CO_CHANGED computation impl](./task-007-co-change-impl.md)
- [Task 008: Graph lifecycle test](./task-008-lifecycle-test.md)
- [Task 008: Graph lifecycle impl](./task-008-lifecycle-impl.md)
- [Task 009: File-level blast radius test](./task-009-blast-radius-file-test.md)
- [Task 009: File-level blast radius impl](./task-009-blast-radius-file-impl.md)
- [Task 010: P0 CLI wiring](./task-010-cli-p0.md)
- [Task 011: P0 E2E tests](./task-011-e2e-p0-test.md)
- [Task 011: P0 E2E impl](./task-011-e2e-p0-impl.md)
- [Task 012: Tree-sitter parser test](./task-012-treesitter-test.md)
- [Task 012: Tree-sitter parser impl](./task-012-treesitter-impl.md)
- [Task 013: AST indexing test](./task-013-ast-index-test.md)
- [Task 013: AST indexing impl](./task-013-ast-index-impl.md)
- [Task 014: Symbol-level blast radius test](./task-014-blast-radius-symbol-test.md)
- [Task 014: Symbol-level blast radius impl](./task-014-blast-radius-symbol-impl.md)
- [Task 015: Hotspots query test](./task-015-hotspots-test.md)
- [Task 015: Hotspots query impl](./task-015-hotspots-impl.md)
- [Task 016: Ownership query test](./task-016-ownership-test.md)
- [Task 016: Ownership query impl](./task-016-ownership-impl.md)
- [Task 017: Action capture test](./task-017-capture-test.md)
- [Task 017: Action capture impl](./task-017-capture-impl.md)
- [Task 018: Timeline query test](./task-018-timeline-test.md)
- [Task 018: Timeline query impl](./task-018-timeline-impl.md)
- [Task 019: P1 CLI wiring](./task-019-cli-p1.md)
- [Task 020: P1 E2E tests](./task-020-e2e-p1-test.md)
- [Task 020: P1 E2E impl](./task-020-e2e-p1-impl.md)

## BDD Coverage

All 51 non-P2 BDD scenarios from the design are covered across the task files:

| Feature | Scenarios | Tasks |
|---------|-----------|-------|
| Graph Indexing | 11 | 005, 006, 007, 008 |
| Blast Radius Query | 7 | 009, 014 |
| Code Ownership Query | 5 | 016 |
| Change Pattern Query | 5 (excl. 3 P2) | 015 |
| Action Capture | 9 | 017 |
| Timeline | 6 (excl. 1 P2) | 018 |
| Graph Lifecycle | 8 | 008 |

P2 scenarios excluded: Stability module, Stability file, Co-change clusters, Timeline compression fails gracefully, all Diagnose scenarios (4).

## Dependency Chain

```
001 (setup)
 |
 v
002 (domain types)
 |
 +----------+-----------+-----------+
 |          |           |           |
 v          v           v           v
003        004         012         (independent foundation)
(sqlite)   (git)       (treesitter)
 |          |           |
 +----+-----+           |
      |                  |
      v                  |
     005 (index full)    |
      |                  |
 +----+-----+-----+     |
 |    |     |     |      |
 v    v     v     v      v
006  007   015   016    013 (ast index) <-- depends on 005 + 012
 |    |     |     |      |
 |    +--+--+     |      v
 |       |        |     014 (blast radius symbol) <-- depends on 009 + 013
 |       v        |
 |      009       |
 |   (blast file) |
 |       |        |
 v       v        v
008     010 (cli p0) <-- depends on 005, 008, 009
(lifecycle) |
            v
           011 (e2e p0)

017 (capture) <-- depends on 003, 004
 |
 v
018 (timeline) <-- depends on 017

019 (cli p1) <-- depends on 012-018
 |
 v
020 (e2e p1)
```

**Analysis:**
- No circular dependencies
- P0 path: 001 -> 002 -> 003/004 -> 005 -> 006/007/008 -> 009 -> 010 -> 011
- P1 branches from 002 (treesitter), 005 (hotspots/ownership), and 003+004 (capture)
- Maximum parallel width: 4 tasks (003, 004, 012 after 002; or 006, 007, 015, 016 after 005)
- Critical path length: 10 (001 -> 002 -> 003+004 -> 005 -> 007 -> 009 -> 010 -> 011 -> 019 -> 020)

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-04-06-code-graph-plan/`. Execution options:

**1. Orchestrated Execution (Recommended)** - Load `superpowers:executing-plans` skill using the Skill tool.

**2. Direct Agent Team** - Load `superpowers:agent-team-driven-development` skill using the Skill tool.

**3. BDD-Focused Execution** - Load `superpowers:behavior-driven-development` skill using the Skill tool for specific scenarios.
