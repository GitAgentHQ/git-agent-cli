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
| R08 | **Graph storage in `.git-agent/graph.db`** -- KuzuDB database directory alongside existing `.git-agent/config.yml` | Consistent with existing config location; easy to gitignore |
| R09 | **`.gitignore` integration** -- `git-agent init` and `graph index` ensure `graph.db/` is in `.git-agent/.gitignore` or the project `.gitignore` | Graph DB should never be committed |
| R10 | **Graceful handling of empty/missing graph** -- commands that query the graph return a clear error with hint to run `graph index` first | Agents need actionable error messages |

### P1 -- Important, can ship after v1

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

### P2 -- Future / nice-to-have

| # | Requirement | Rationale |
|---|-------------|-----------|
| R19 | **`git-agent graph coupling <pathA> <pathB>`** -- return coupling score between two files/directories | Quantifies hidden dependencies |
| R20 | **`git-agent graph stability <path>`** -- return change velocity metrics for a file/directory over time | Identifies volatile vs stable code |
| R21 | **Time-windowed queries** -- `--since` and `--until` flags on all query commands | Focus analysis on recent history |
| R22 | **Graph export** -- dump graph as DOT or Mermaid for visualization | Debugging and documentation |
| R23 | **Watch mode** -- auto-reindex on new commits (via filesystem watcher or post-commit hook) | Zero-friction for long-running agent sessions |
| R24 | **MCP server mode** -- expose graph queries as MCP tools | Native integration for MCP-aware agents |

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
| Binary size increase is bounded | Delta under 15MB from KuzuDB + Tree-sitter combined |
| No regression in commit/init commands | Existing e2e tests pass unchanged |

---

## 4. Constraints

### C1: CGo from go-kuzu

`go-kuzu` (the Go binding for KuzuDB) uses CGo to call the KuzuDB C/C++ library. This has concrete implications:

- **Cross-compilation becomes harder** -- `CGO_ENABLED=1` is required; need platform-specific KuzuDB shared libraries for each target (darwin-arm64, darwin-amd64, linux-amd64, linux-arm64).
- **CI must install KuzuDB native libs** -- GitHub Actions runners need the KuzuDB shared library available at build time.
- **Build time increases** -- CGo compilation is slower than pure Go.
- **Current binary is 7.2MB** -- KuzuDB embedded will add the shared library weight.

**Mitigation**: The `graph` subcommand group is isolated. The graph infrastructure package links KuzuDB; the rest of the binary remains pure Go. Build tags (`//go:build graph`) can make the graph feature opt-in at compile time, keeping the default build lean.

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
infrastructure/graph/ -- KuzuDB adapter, Tree-sitter adapter, git history walker
```

Domain must have zero external imports. KuzuDB and Tree-sitter live exclusively in `infrastructure/`.

### C4: Existing dependency surface

The project currently has exactly 3 direct dependencies (go-openai, cobra, yaml.v3). Adding go-kuzu and gotreesitter roughly doubles the dependency count. Each new dependency must be justified.

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
  index       Build or update the code graph from git history
  blast-radius  Show files/symbols affected by changing a target
  hotspots    Show frequently changed files                     (P1)
  ownership   Show who owns a file or directory                 (P1)
  coupling    Show coupling score between two paths             (P2)
  stability   Show change velocity for a path                   (P2)
  status      Show graph DB metadata (last indexed commit, node/edge counts)
  reset       Delete the graph DB and start fresh
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

#### `git-agent graph status`

```bash
$ git-agent graph status
{
  "exists": true,
  "last_indexed_commit": "a958f19",
  "last_indexed_at": "2026-04-02T10:30:00Z",
  "commits_behind": 3,
  "node_counts": {"commit": 4835, "file": 349, "author": 12, "symbol": 0},
  "edge_counts": {"modifies": 18420, "authored": 4835, "co_changed": 2847, "contains": 0, "calls": 0, "imports": 0},
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
    "symbol": 0
  },
  "edge_counts": {
    "modifies": 18420,
    "authored": 4835,
    "co_changed": 2847,
    "contains": 0,
    "calls": 0,
    "imports": 0
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
| T1 | **go-kuzu CGo breaks cross-compilation** -- KuzuDB's C++ library must be available for each target platform at build time | High | High | Use build tags (`//go:build graph`) so default builds remain pure Go. Provide platform-specific CI jobs. Distribute pre-built binaries per platform. Consider: if CGo cost is too high, evaluate SQLite + custom schema as fallback. |
| T2 | **KuzuDB binary size bloat** -- the embedded KuzuDB library adds 20-40MB to the binary | High | Medium | Accept the size for graph-enabled builds. Default `make build` excludes graph via build tag. `make build-graph` includes it. Document the tradeoff. |
| T3 | **go-kuzu v0.11.3 maturity** -- the Go binding is relatively young; API may change or have bugs | Medium | High | Pin the exact version. Write an adapter interface in `domain/graph/` so the KuzuDB implementation can be swapped. Add integration tests that exercise KuzuDB directly. |
| T4 | **Large repo indexing performance** -- repos with 50k+ commits may take minutes to index | Medium | Medium | Batch git log parsing (1000 commits at a time). Use KuzuDB bulk insert. Show progress bar on stderr. The `--max-commits` flag provides an escape hatch. |
| T5 | **Co-change combinatorial explosion** -- a commit touching 100 files generates 4950 CO_CHANGED edges | Medium | Medium | Cap CO_CHANGED generation: skip commits touching more than 50 files (likely merge commits or bulk reformats). Make the threshold configurable via `--max-files-per-commit`. |
| T6 | **Tree-sitter grammar coverage gaps** -- some languages or language features may not parse correctly | Low | Low | P1 scope. Start with Go + TypeScript. Each grammar is independently toggleable. Fallback: file-level analysis always works even without AST. |
| T7 | **Graph DB corruption** -- crash during indexing leaves DB in inconsistent state | Low | Medium | Use KuzuDB transactions. Wrap each commit batch in a transaction. `graph reset` provides manual recovery. Store last-indexed commit only after transaction commits. |
| T8 | **Concurrent access** -- multiple agent sessions or terminal tabs run graph commands simultaneously | Low | Medium | KuzuDB supports concurrent reads. Use file locking for write operations (indexing). Return clear error if lock cannot be acquired. |
| T9 | **Scope creep into MCP/HTTP** -- temptation to add server mode before CLI is solid | Medium | Medium | Hard boundary: v1 is CLI-only (R24 is P2). MCP is a separate feature after CLI UX is validated with real agent workflows. |
| T10 | **Breaking existing builds for non-graph users** -- CGo requirement leaks into default build | Medium | High | Build tag isolation is critical. `go test ./...` must pass without CGo by default. Graph tests live behind build tags or in a separate module. CI runs both builds. |

---

## Appendix: Graph Schema (KuzuDB Cypher)

> **Authoritative schema**: See [_index.md](./_index.md) for the complete DDL.
> This appendix is a simplified overview.

```cypher
-- Node tables
CREATE NODE TABLE Commit(hash STRING PRIMARY KEY, message STRING, author_name STRING, author_email STRING, timestamp INT64, parent_hashes STRING[])
CREATE NODE TABLE File(path STRING PRIMARY KEY, language STRING, last_indexed_hash STRING)
CREATE NODE TABLE Author(email STRING PRIMARY KEY, name STRING)
CREATE NODE TABLE Symbol(id STRING PRIMARY KEY, name STRING, kind STRING, file_path STRING, start_line INT64, end_line INT64, signature STRING)
CREATE NODE TABLE IndexState(key STRING PRIMARY KEY, value STRING)

-- Relationship tables
CREATE REL TABLE MODIFIES(FROM Commit, TO File, additions INT64, deletions INT64, status STRING)
CREATE REL TABLE AUTHORED(FROM Author, TO Commit)
CREATE REL TABLE CO_CHANGED(FROM File, TO File, coupling_count INT64, coupling_strength DOUBLE, last_coupled_hash STRING)
CREATE REL TABLE CONTAINS(FROM File, TO Symbol)
CREATE REL TABLE CALLS(FROM Symbol, TO Symbol, confidence DOUBLE)
CREATE REL TABLE IMPORTS(FROM File, TO File, import_path STRING)
```

`Symbol.id` is a synthetic key: `{file_path}:{kind}:{name}:{start_line}` to handle overloaded names.

`CO_CHANGED.coupling_count` is updated incrementally: on each new commit, for each pair of files in the commit, upsert the edge and increment count. `coupling_strength` = coupling_count / max(commits_file_a, commits_file_b).

`MODIFIES.status` mirrors git's status codes: `A` (added), `M` (modified), `D` (deleted), `R` (renamed).
