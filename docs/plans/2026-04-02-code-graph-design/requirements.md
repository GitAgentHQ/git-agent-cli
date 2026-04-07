# git-agent graph -- Context & Requirements

## 1. Problem Statement

Coding agents (Claude Code, Cursor, Windsurf) operate on individual files but lack structural awareness of the codebase as a living system. When an agent needs to answer "what will break if I change this function?", it has three inadequate options today:

1. **Read `git log`** -- flat text, no relationships, requires the agent to parse and correlate manually. O(n) per query across commit history.
2. **Grep the codebase** -- finds textual references but misses semantic relationships (a function renamed, an interface implemented in a different package, a file that always changes alongside another).
3. **Read the whole codebase** -- context window limits make this impossible for repos above a few thousand lines.

A graph database over git history + AST structure solves this by pre-computing relationships that agents can query in O(1) lookups:

- **Blast radius**: File X imports module Y which is called by 14 other files -- here they are, ranked by co-change frequency.
- **Ownership**: This file was last touched by 3 authors, most recently 2 days ago.
- **Hotspots**: These 5 files change together in 80% of commits -- they are a hidden module boundary.
- **Stability**: This directory has not changed in 6 months vs. this one changes every week.

The graph lives locally (`.git-agent/graph.db`), is incrementally maintained, and is queryable via CLI subcommands that return machine-parseable JSON.

---

## 2. Requirements

### P0 -- Must ship in v1

| # | Requirement | Rationale |
|---|-------------|-----------|
| R01 | **Index git history into graph DB** -- parse commits, extract file modifications, author info, and store as Commit, File, Author nodes with MODIFIES and AUTHORED edges | Foundation for all queries |
| R02 | **Incremental indexing** -- track last indexed commit hash, only process new commits on subsequent runs | Repos with 10k+ commits must not re-index from scratch every time |
| R03 | **Co-change detection** -- compute CO_CHANGED edges between files that appear in the same commit, with a `count` weight | Core signal for blast radius |
| R04 | **Blast radius query (file-level)** -- given a file path, return co-changed files ranked by frequency, plus direct importers/callers | Primary use case from discovery |
| R05 | **JSON output for all queries** -- every subcommand outputs structured JSON to stdout; human-readable summaries to stderr when `--format=text` | Agents parse stdout; humans read stderr |
| R06 | **`git-agent graph index` subcommand** -- triggers indexing, reports progress to stderr | Entry point for building the graph |
| R07 | **`git-agent graph blast-radius <path>` subcommand** -- executes the blast radius query | Primary agent-facing command |
| R08 | **Graph storage in `.git-agent/graph.db`** -- SQLite database file alongside existing `.git-agent/config.yml` | Consistent with existing config location; easy to gitignore |
| R09 | **`.gitignore` integration** -- `git-agent init` and `graph index` ensure `graph.db` is in `.git-agent/.gitignore` or the project `.gitignore` | Graph DB should never be committed |
| R10 | **Graceful handling of empty/missing graph** -- commands that query the graph return a clear error with hint to run `graph index` first | Agents need actionable error messages |

### P1a -- AST + Symbols (can ship after v1)

| # | Requirement | Rationale |
|---|-------------|-----------|
| R11 | **AST parsing via Tree-sitter** -- extract Symbol nodes (functions, classes, methods) and CONTAINS edges (File -> Symbol) | Enables function-level blast radius |
| R12 | **CALLS edges** -- detect function call relationships within and across files | Enables "what calls this function?" |
| R13 | **IMPORTS edges** -- detect file/module import relationships | Enables "what depends on this module?" |
| R14 | **Blast radius query (symbol-level)** -- given a function/class name, return callers and co-changed symbols | Higher precision than file-level |
| R15 | **`git-agent graph hotspots` subcommand** -- return files ranked by change frequency, optionally filtered by time window | Change frequency is a strong code smell signal |
| R16 | **`git-agent graph ownership <path>` subcommand** -- return authors ranked by contribution to a file/directory | Helps agents know who to tag for review |
| R17 | **Incremental AST re-parsing** -- only re-parse files that changed since last index | Performance: avoid re-parsing entire codebase |
| R18 | **Multi-language AST support** -- Go, TypeScript, Python, Rust, Java at minimum | Cover the most common languages agents work with |

### P1b -- Action Capture + Timeline (can ship after P1a)

| # | Requirement | Rationale |
|---|-------------|-----------|
| R19 | **`git-agent graph capture` subcommand** -- record an agent or human action (diff + metadata) into the graph as a Session/Action node | Foundation for timeline and diagnose; called by agent hooks |
| R20 | **Session tracking** -- group sequential actions from the same source into sessions, with automatic timeout-based lifecycle | Provides context grouping for timeline display |
| R21 | **Agent hook integration** -- Claude Code `PostToolUse` hook calls `graph capture` after each `Edit`/`Write`/`Bash` tool call | Primary mechanism for capturing agent actions at tool-call granularity |
| R22 | **`git-agent graph timeline` subcommand** -- display session/action history, filterable by time, source, and file | Human-readable history of what agents and humans did |
| R23 | **Action-to-file attribution** -- action_modifies rows link each action to the files it changed, with addition/deletion counts | Enables per-action impact analysis |
| R24 | **Action-to-commit linking** -- when `git-agent commit` runs, link preceding uncommitted actions to the resulting Commit node via action_produces | Bridges action-level and commit-level history |

### P2 -- Future / nice-to-have

| # | Requirement | Rationale |
|---|-------------|-----------|
| R25 | **LLM timeline compression** -- `graph timeline --compress` sends grouped actions to LLM and returns human-readable session summaries | Raw diffs are noisy; compressed timeline is what humans want to read |
| R26 | **`git-agent graph diagnose` subcommand** -- given a bug description or file path, combine blast-radius + action timeline + LLM reasoning to identify the most likely introducing action and suggest a fix | AI-enhanced `git bisect` at action granularity |
| R27 | **`git-agent graph coupling <pathA> <pathB>`** -- return coupling score between two files/directories | Quantifies hidden dependencies |
| R28 | **`git-agent graph stability <path>`** -- return change velocity metrics for a file/directory over time | Identifies volatile vs stable code |
| R29 | **Time-windowed queries** -- `--since` and `--until` flags on all query commands | Focus analysis on recent history |
| R30 | **Graph export** -- dump graph as DOT or Mermaid for visualization | Debugging and documentation |
| R31 | **Watch mode** -- auto-reindex on new commits (via filesystem watcher or post-commit hook) | Zero-friction for long-running agent sessions |
| R32 | **MCP server mode** -- expose graph queries as MCP tools | Native integration for MCP-aware agents |

---

## 3. Success Criteria

### Functional

| Criterion | Measurement |
|-----------|------------|
| Index a 5,000-commit repo with <100 files | Completes in under 30 seconds on first run |
| Incremental index after 1 new commit | Completes in under 2 seconds |
| Blast radius query returns results | Response in under 500ms for a repo with 10k file nodes |
| Agent integration works end-to-end | Claude Code can call `git-agent graph blast-radius src/main.go`, parse JSON output, and use it to inform a refactoring decision |
| All queries return valid JSON | Output passes `jq .` validation; schema is stable across minor versions |

### Quality

| Criterion | Measurement |
|-----------|------------|
| Existing tests still pass | `make test` green after graph feature merge |
| Graph feature has unit + e2e tests | Coverage for all P0 requirements |
| Binary size increase is bounded | Total under 35MB (modernc.org/sqlite ~8-12MB + gotreesitter ~5-10MB) |
| No regression in commit/init commands | Existing e2e tests pass unchanged |

---

## 4. Constraints

### C1: Pure Go SQLite (modernc.org/sqlite)

`modernc.org/sqlite` is a pure Go transpilation of SQLite. Zero CGo, full cross-compilation support. Implications:

- **Binary size**: adds ~8-12MB (the transpiled SQLite C code compiled to Go).
- **Performance**: ~1.1-2x slower than CGo-based `mattn/go-sqlite3`. Acceptable for a CLI tool indexing repos of typical size.
- **No platform-specific build steps**: `GOOS`/`GOARCH` work out of the box.
- **No build tags needed**: graph feature compiles unconditionally into every binary.

### C2: gotreesitter is pure Go (P1, no CGo)

`gotreesitter` v0.6.4 uses pure Go grammar implementations (WASM compiled to Go). No CGo penalty for the AST layer. However:

- Binary size: each grammar adds ~1-3MB. Supporting 5 languages could add 5-15MB.
- Parse speed: pure Go Tree-sitter is ~3-5x slower than native C Tree-sitter. Acceptable for incremental indexing but rules out real-time parsing.

### C3: Clean Architecture compliance

The graph feature must follow the existing 4-layer structure:

```
cmd/graph.go          -- Cobra wiring, flag parsing, JSON output formatting
application/graph_*.go -- GraphService orchestration
domain/graph/         -- Node, Edge, Relationship interfaces and value objects
infrastructure/graph/ -- SQLite adapter, Tree-sitter adapter, git history walker
```

Domain must have zero external imports. SQLite and Tree-sitter live exclusively in `infrastructure/`.

### C4: Existing dependency surface

The project currently has exactly 3 direct dependencies (go-openai, cobra, yaml.v3). Adding modernc.org/sqlite and gotreesitter increases the dependency count. Each new dependency must be justified.

### C5: Storage budget

`.git-agent/graph.db` size target:
- A 10k-commit, 500-file repo should produce a graph DB under 50MB.
- A 1k-commit, 100-file repo should produce a graph DB under 10MB.

### C6: No network requirement

Graph indexing and querying must work entirely offline. No LLM calls. No API calls. Pure local computation over local git history and local source files.

---

## 5. CLI UX Design

### Subcommand tree

```
git-agent graph
  index         Build or update the code graph from git history
  blast-radius  Show files/symbols affected by changing a target
  capture       Record an agent/human action into the graph       (P1b)
  timeline      Show session/action history                       (P1b)
  hotspots      Show frequently changed files                     (P1a)
  ownership     Show who owns a file or directory                 (P1a)
  diagnose      Trace a bug to its introducing action             (P2)
  coupling      Show coupling score between two paths             (P2)
  stability     Show change velocity for a path                   (P2)
  clusters      Show co-change clusters                           (P2)
  status        Show graph DB metadata (last indexed commit, node/edge counts)
  reset         Delete the graph DB and start fresh
```

### Command examples and agent usage patterns

#### `git-agent graph index`

```bash
# First-time full index
$ git-agent graph index
{"indexed_commits":4832,"new_commits":4832,"files":347,"authors":12,"duration_ms":18420}

# Incremental update
$ git-agent graph index
{"indexed_commits":4835,"new_commits":3,"files":349,"authors":12,"duration_ms":890}

# Limit history depth
$ git-agent graph index --max-commits 1000

# Force full re-index
$ git-agent graph index --force

# Include AST parsing (P1)
$ git-agent graph index --ast
{"indexed_commits":4835,"new_commits":0,"files":349,"symbols":2847,"authors":12,"duration_ms":45200}
```

**Agent pattern**: Run `git-agent graph index` at session start, then query throughout the session.

#### `git-agent graph blast-radius`

```bash
# File-level blast radius
$ git-agent graph blast-radius src/application/commit_service.go
{
  "target": "src/application/commit_service.go",
  "target_type": "file",
  "co_changed": [
    {"path": "src/cmd/commit.go", "coupling_count": 42, "coupling_strength": 0.78},
    {"path": "src/application/commit_service_test.go", "coupling_count": 38, "coupling_strength": 0.70},
    {"path": "src/domain/commit/generator.go", "coupling_count": 15, "coupling_strength": 0.28}
  ],
  "importers": [
    {"path": "src/cmd/commit.go", "relationship": "imports"}
  ],
  "total_co_changed": 12,
  "query_ms": 8
}

# With depth limit
$ git-agent graph blast-radius src/domain/commit/ --depth 2

# Symbol-level (P1, requires --ast index)
$ git-agent graph blast-radius --symbol "CommitService.Commit"
{
  "target": "CommitService.Commit",
  "target_type": "symbol",
  "file": "src/application/commit_service.go",
  "callers": [
    {"symbol": "runCommit", "file": "src/cmd/commit.go", "relationship": "calls"}
  ],
  "co_changed_symbols": [
    {"symbol": "CommitService.plan", "file": "src/application/commit_service.go", "count": 30}
  ],
  "query_ms": 12
}

# Top N results only
$ git-agent graph blast-radius src/main.go --top 5

# Minimum co-change threshold
$ git-agent graph blast-radius src/main.go --min-count 3
```

**Agent pattern**: Before modifying a file, run blast-radius to understand downstream impact. Include affected files in the change plan.

#### `git-agent graph hotspots` (P1)

```bash
$ git-agent graph hotspots --top 10
{
  "hotspots": [
    {"path": "src/application/commit_service.go", "changes": 87, "authors": 3, "last_changed": "2026-03-30"},
    {"path": "src/cmd/commit.go", "changes": 65, "authors": 2, "last_changed": "2026-03-29"}
  ],
  "query_ms": 5
}

# Filter by directory
$ git-agent graph hotspots --path src/infrastructure/ --top 5

# Time window
$ git-agent graph hotspots --since 2026-01-01
```

**Agent pattern**: At session start, identify hotspots to flag high-risk areas before making changes.

#### `git-agent graph ownership` (P1)

```bash
$ git-agent graph ownership src/application/commit_service.go
{
  "path": "src/application/commit_service.go",
  "authors": [
    {"name": "Frad LEE", "email": "fradser@gmail.com", "commits": 45, "contribution_ratio": 0.82, "last_commit": "2026-03-30"},
    {"name": "Bot", "email": "bot@example.com", "commits": 10, "contribution_ratio": 0.18, "last_commit": "2026-03-15"}
  ],
  "total_commits": 55,
  "query_ms": 3
}

# Directory-level ownership
$ git-agent graph ownership src/domain/
```

**Agent pattern**: Before suggesting changes to unfamiliar code, check who owns it to inform review recommendations.

#### `git-agent graph capture` (P1)

```bash
# Called by Claude Code PostToolUse hook after an Edit tool call
$ git-agent graph capture --source claude-code --tool Edit
{"action_id":"s1:3","session_id":"s1","files_changed":["src/application/commit_service.go"],"capture_ms":45}

# Called by hook after a Bash tool call (e.g., running a test that modified files)
$ git-agent graph capture --source claude-code --tool Bash
{"action_id":"s1:4","session_id":"s1","files_changed":[],"capture_ms":12}

# No diff detected -- no-op
$ git-agent graph capture --source claude-code --tool Edit
{"action_id":null,"skipped":true,"reason":"no changes detected"}

# End a session explicitly
$ git-agent graph capture --source claude-code --end-session
{"session_id":"s1","ended":true,"total_actions":4}

# With a human-readable message
$ git-agent graph capture --source human --message "fixed auth middleware"
{"action_id":"s2:1","session_id":"s2","files_changed":["src/middleware/auth.go"]}
```

**Agent pattern**: Configured as a `PostToolUse` hook. Runs automatically after every `Edit`/`Write`/`Bash` call. The agent does not invoke this directly.

#### `git-agent graph timeline` (P1)

```bash
# Raw timeline (offline, no LLM)
$ git-agent graph timeline --since 2h
{
  "sessions": [
    {
      "id": "s1",
      "source": "claude-code",
      "started_at": "2026-04-06T14:02:00Z",
      "ended_at": "2026-04-06T14:15:00Z",
      "actions": [
        {"id": "s1:1", "tool": "Edit", "timestamp": "2026-04-06T14:02:12Z", "files": ["src/application/commit_service.go"], "summary": null},
        {"id": "s1:2", "tool": "Write", "timestamp": "2026-04-06T14:03:45Z", "files": ["src/application/commit_service_test.go"], "summary": null},
        {"id": "s1:3", "tool": "Bash", "timestamp": "2026-04-06T14:05:00Z", "files": [], "summary": null}
      ],
      "summary": null
    },
    {
      "id": "s2",
      "source": "human",
      "started_at": "2026-04-06T14:20:00Z",
      "actions": [
        {"id": "s2:1", "tool": "manual-save", "timestamp": "2026-04-06T14:20:30Z", "files": ["src/cmd/commit.go"], "summary": null}
      ],
      "summary": null
    }
  ],
  "total_sessions": 2,
  "total_actions": 4,
  "query_ms": 28
}

# Compressed timeline (requires LLM)
$ git-agent graph timeline --since 2h --compress
{
  "sessions": [
    {
      "id": "s1",
      "source": "claude-code",
      "started_at": "2026-04-06T14:02:00Z",
      "ended_at": "2026-04-06T14:15:00Z",
      "summary": "Refactored CommitService to extract hook retry logic into a separate method, added 3 unit tests",
      "action_count": 3
    },
    {
      "id": "s2",
      "source": "human",
      "started_at": "2026-04-06T14:20:00Z",
      "summary": "Fixed typo in commit command error message",
      "action_count": 1
    }
  ],
  "total_sessions": 2,
  "total_actions": 4,
  "query_ms": 3200
}

# Filter by file
$ git-agent graph timeline --file src/application/commit_service.go

# Filter by source
$ git-agent graph timeline --source claude-code --since 1d
```

**Agent pattern**: Before starting work, review `graph timeline --since 1d --compress` to understand recent changes and avoid conflicts.

#### `git-agent graph diagnose` (P2)

```bash
# Diagnose by bug description
$ git-agent graph diagnose "hook validation fails on messages with colons"
{
  "suspects": [
    {
      "action_id": "s1:2",
      "session_id": "s1",
      "source": "claude-code",
      "tool": "Edit",
      "timestamp": "2026-04-06T14:03:45Z",
      "file": "src/domain/validation.go",
      "diff_excerpt": "- if !strings.Contains(title, \":\") {\n+ if !strings.Contains(title, \": \") {",
      "confidence": 0.85,
      "explanation": "This action changed the colon check from ':' to ': ' (with trailing space), which would cause messages with colons but no space to fail validation"
    }
  ],
  "blast_radius": ["src/cmd/commit.go", "src/application/commit_service.go"],
  "suggested_fix": "Revert the space requirement in the colon check, or update the validation to accept both ':' and ': '",
  "query_ms": 8500
}

# Diagnose by file path (finds recent regressions in that file)
$ git-agent graph diagnose src/domain/validation.go --since 3d

# Deeper trace
$ git-agent graph diagnose "tests fail after refactoring" --depth 5
```

**Agent pattern**: When a test fails or behavior regresses, run `graph diagnose` before manually reading diffs. The agent gets a ranked list of suspect actions with explanations.

#### `git-agent graph status`

```bash
$ git-agent graph status
{
  "exists": true,
  "last_indexed_commit": "a958f19",
  "last_indexed_at": "2026-04-02T10:30:00Z",
  "commits_behind": 3,
  "node_counts": {"commit": 4835, "file": 349, "author": 12, "symbol": 0, "session": 23, "action": 187},
  "edge_counts": {"modifies": 18420, "authored": 4835, "co_changed": 2847, "contains": 0, "calls": 0, "imports": 0, "action_modifies": 312, "action_produces": 45},
  "db_size_bytes": 12582912,
  "ast_indexed": false
}

# When no graph exists
$ git-agent graph status
{
  "exists": false,
  "hint": "run 'git-agent graph index' to build the graph"
}
```

**Agent pattern**: Check status to decide whether to run `graph index` before querying.

#### `git-agent graph reset`

```bash
$ git-agent graph reset
{"deleted": true, "freed_bytes": 12582912}
```

### Global flags (inherited from root)

All `graph` subcommands inherit:
- `--verbose` / `-v` -- detailed progress to stderr
- `--format text|json` -- default `json`; `text` writes human-readable to stderr, JSON to stdout

### Exit codes

Consistent with existing conventions in `pkg/errors/`:
- `0` -- success
- `1` -- general error (no git repo, DB corruption, invalid arguments)
- `3` -- graph not indexed (distinct from hook-blocked `2`; agents can detect and auto-run `graph index`)

---

## 6. Output Format Specification

All JSON output follows these conventions:

1. **Top-level object** -- never a bare array. Always `{"key": [...]}`.
2. **Snake_case keys** -- consistent with Go JSON conventions used elsewhere in the project.
3. **Paths are repo-relative** -- never absolute. Same as `git diff --name-status` output.
4. **Timestamps are RFC 3339** -- `2026-04-02T10:30:00Z`.
5. **Durations are in milliseconds** -- integer, key suffix `_ms`.
6. **Counts are integers** -- never floats, except for `ratio` (0.0-1.0).

### Schema per query type

#### Index result

```json
{
  "indexed_commits": 4835,
  "new_commits": 3,
  "files": 349,
  "symbols": 0,
  "authors": 12,
  "duration_ms": 890,
  "last_commit": "a958f19"
}
```

#### Blast radius result

```json
{
  "target": "src/application/commit_service.go",
  "target_type": "file",
  "co_changed": [
    {
      "path": "src/cmd/commit.go",
      "coupling_count": 42,
      "coupling_strength": 0.78
    }
  ],
  "importers": [
    {
      "path": "src/cmd/commit.go",
      "relationship": "imports"
    }
  ],
  "callers": [],
  "total_co_changed": 12,
  "query_ms": 8
}
```

Fields `importers` and `callers` are empty arrays (not absent) when AST is not indexed. This lets agents distinguish "no callers found" from "callers not available."

#### Hotspots result (P1)

```json
{
  "hotspots": [
    {
      "path": "src/application/commit_service.go",
      "changes": 87,
      "authors": 3,
      "last_changed": "2026-03-30"
    }
  ],
  "total": 347,
  "query_ms": 5
}
```

#### Ownership result (P1)

```json
{
  "path": "src/application/commit_service.go",
  "authors": [
    {
      "name": "Frad LEE",
      "email": "fradser@gmail.com",
      "commits": 45,
      "contribution_ratio": 0.82,
      "last_commit": "2026-03-30"
    }
  ],
  "total_commits": 55,
  "query_ms": 3
}
```

#### Capture result (P1)

```json
{
  "action_id": "s1:3",
  "session_id": "s1",
  "files_changed": ["src/application/commit_service.go"],
  "capture_ms": 45
}
```

When no diff is detected: `{"action_id": null, "skipped": true, "reason": "no changes detected"}`.

#### Timeline result (P1)

```json
{
  "sessions": [
    {
      "id": "s1",
      "source": "claude-code",
      "started_at": "2026-04-06T14:02:00Z",
      "ended_at": "2026-04-06T14:15:00Z",
      "action_count": 3,
      "summary": null,
      "actions": [
        {
          "id": "s1:1",
          "tool": "Edit",
          "timestamp": "2026-04-06T14:02:12Z",
          "files": ["src/application/commit_service.go"],
          "summary": null
        }
      ]
    }
  ],
  "total_sessions": 1,
  "total_actions": 3,
  "query_ms": 28
}
```

When `--compress` is used, `actions` array is omitted and `summary` is filled with an LLM-generated description.

#### Diagnose result (P2)

```json
{
  "suspects": [
    {
      "action_id": "s1:2",
      "session_id": "s1",
      "source": "claude-code",
      "tool": "Edit",
      "timestamp": "2026-04-06T14:03:45Z",
      "file": "src/domain/validation.go",
      "diff_excerpt": "- if !strings.Contains(title, \":\")\n+ if !strings.Contains(title, \": \")",
      "confidence": 0.85,
      "explanation": "Changed colon check from ':' to ': ', causing messages without trailing space to fail"
    }
  ],
  "blast_radius": ["src/cmd/commit.go", "src/application/commit_service.go"],
  "suggested_fix": "Revert the space requirement or accept both formats",
  "query_ms": 8500
}
```

#### Status result

```json
{
  "exists": true,
  "last_indexed_commit": "a958f19",
  "last_indexed_at": "2026-04-02T10:30:00Z",
  "commits_behind": 3,
  "node_counts": {
    "commit": 4835,
    "file": 349,
    "author": 12,
    "symbol": 0,
    "session": 23,
    "action": 187
  },
  "edge_counts": {
    "modifies": 18420,
    "authored": 4835,
    "co_changed": 2847,
    "contains": 0,
    "calls": 0,
    "imports": 0,
    "action_modifies": 312,
    "action_produces": 45
  },
  "db_size_bytes": 12582912,
  "ast_indexed": false
}
```

#### Error result

```json
{
  "error": "graph not indexed",
  "hint": "run 'git-agent graph index' to build the graph",
  "exit_code": 3
}
```

---

## 7. Risk Register

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| T1 | **SQLite recursive CTE performance** -- complex graph traversals (multi-hop call chains) may be slower than a dedicated graph DB | Low | Medium | Per-repo graphs are small (thousands of nodes). Benchmark against real repos. Add depth limits to all recursive CTEs. Optimize with proper indexes. |
| T2 | **SQLite binary size** -- modernc.org/sqlite adds ~8-12MB to the binary | Low | Low | Acceptable for a modern CLI tool. Total binary ~20-30MB with gotreesitter. No build tag separation needed. |
| T3 | **modernc.org/sqlite compatibility** -- transpiled SQLite may have edge-case differences from native SQLite | Low | Low | modernc.org/sqlite tracks upstream SQLite closely (v3.51.x). 3,400+ importers in production. The adapter interface in `domain/graph/` allows swapping implementations. |
| T4 | **Large repo indexing performance** -- repos with 50k+ commits may take minutes to index | Medium | Medium | Batch git log parsing (1000 commits at a time). Use SQLite transactions with batch inserts. Show progress bar on stderr. The `--max-commits` flag provides an escape hatch. |
| T5 | **Co-change combinatorial explosion** -- a commit touching 100 files generates 4950 CO_CHANGED edges | Medium | Medium | Cap CO_CHANGED generation: skip commits touching more than 50 files (likely merge commits or bulk reformats). Make the threshold configurable via `--max-files-per-commit`. |
| T6 | **Tree-sitter grammar coverage gaps** -- some languages or language features may not parse correctly | Low | Low | P1a scope. Start with Go + TypeScript. Each grammar is independently toggleable. Fallback: file-level analysis always works even without AST. |
| T7 | **Graph DB corruption** -- crash during indexing leaves DB in inconsistent state | Low | Medium | Use SQLite transactions (WAL mode). Wrap each commit batch in a transaction. `graph reset` provides manual recovery. Store last-indexed commit only after transaction commits. |
| T8 | **Concurrent access** -- multiple agent sessions or terminal tabs run graph commands simultaneously | Low | Medium | SQLite WAL mode supports concurrent reads with a single writer. Built-in busy timeout (5s) handles contention. Return clear error if lock cannot be acquired. |
| T9 | **Scope creep into MCP/HTTP** -- temptation to add server mode before CLI is solid | Medium | Medium | Hard boundary: v1 is CLI-only (R32 is P2). MCP is a separate feature after CLI UX is validated with real agent workflows. |
| T10 | ~~**Breaking existing builds**~~ | -- | -- | Resolved: pure Go SQLite eliminates CGo. No build tags needed. Single binary, single build, single test invocation. |
| T11 | **`graph capture` latency blocks agent** -- if capture takes >200ms, it degrades agent UX as a PostToolUse hook | Medium | High | Capture must be fast: read `git diff`, write 2-3 rows, exit. No LLM calls. No schema recomputation. SQLite WAL mode enables fast concurrent writes. |
| T12 | **Diff storage bloat** -- storing full diffs for every action can grow the DB rapidly in long sessions | Medium | Medium | Truncate diffs over 100KB. Purge action data older than 30 days via `graph reset --actions-before DATE`. Track action row count in `status`. |
| T13 | **Claude Code hook API changes** -- hook configuration format may evolve across Claude Code versions | Low | Medium | Keep hook setup in documentation, not hardcoded. Provide a `git-agent graph setup-hooks --agent claude-code` command that generates the correct config. |
| T14 | **LLM dependency for compress/diagnose** -- these commands fail without LLM access, breaking offline expectation | Medium | Low | Clearly document that `--compress` and `diagnose` require LLM. All other graph commands remain fully offline. Use the existing git-agent OpenAI-compatible endpoint config. |
| T15 | **Session boundary heuristics** -- 30-minute timeout may not match real agent session boundaries | Low | Low | Make timeout configurable in `.git-agent/config.yml`. Allow explicit `--end-session` and `--session ID` flags for manual control. |

---

## Appendix: Graph Schema (SQLite DDL)

> **Authoritative schema**: See [_index.md](./_index.md) for the complete DDL
> with all tables, indexes, and constraints. This appendix is a simplified overview.

```sql
-- Node tables
CREATE TABLE commits (hash TEXT PRIMARY KEY, message TEXT, author_name TEXT, author_email TEXT, timestamp INTEGER, parent_hashes TEXT);
CREATE TABLE files (path TEXT PRIMARY KEY, language TEXT, last_indexed_hash TEXT);
CREATE TABLE authors (email TEXT PRIMARY KEY, name TEXT);
CREATE TABLE symbols (id TEXT PRIMARY KEY, name TEXT, kind TEXT, file_path TEXT, start_line INTEGER, end_line INTEGER, signature TEXT);
CREATE TABLE index_state (key TEXT PRIMARY KEY, value TEXT);

-- Edge tables (relationships)
CREATE TABLE modifies (commit_hash TEXT, file_path TEXT, additions INTEGER, deletions INTEGER, status TEXT, PRIMARY KEY (commit_hash, file_path));
CREATE TABLE authored (author_email TEXT, commit_hash TEXT, PRIMARY KEY (author_email, commit_hash));
CREATE TABLE co_changed (file_a TEXT, file_b TEXT, coupling_count INTEGER, coupling_strength REAL, last_coupled_hash TEXT, PRIMARY KEY (file_a, file_b), CHECK (file_a < file_b));
CREATE TABLE contains_symbol (file_path TEXT, symbol_id TEXT, PRIMARY KEY (file_path, symbol_id));
CREATE TABLE calls (from_symbol TEXT, to_symbol TEXT, confidence REAL, PRIMARY KEY (from_symbol, to_symbol));
CREATE TABLE imports (from_file TEXT, to_file TEXT, import_path TEXT, PRIMARY KEY (from_file, to_file));
```

`symbols.id` is a composite key: `{file_path}:{kind}:{name}:{start_line}` to handle overloaded names.

`co_changed.coupling_strength` = coupling_count / max(commits_file_a, commits_file_b). The `CHECK (file_a < file_b)` constraint ensures canonical ordering (each pair stored once).

`modifies.status` mirrors git's status codes: `A` (added), `M` (modified), `D` (deleted), `R` (renamed).
