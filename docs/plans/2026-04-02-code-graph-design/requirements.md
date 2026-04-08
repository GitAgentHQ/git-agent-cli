# git-agent graph -- Context & Requirements

## 1. Problem Statement

Coding agents (Claude Code, Cursor, Windsurf) operate on individual files but lack structural awareness of the codebase as a living system. When an agent needs to answer "what will break if I change this function?", it has three inadequate options today:

1. **Read `git log`** -- flat text, no relationships, requires the agent to parse and correlate manually. O(n) per query across commit history.
2. **Grep the codebase** -- finds textual references but misses semantic relationships (a function renamed, an interface implemented in a different package, a file that always changes alongside another).
3. **Read the whole codebase** -- context window limits make this impossible for repos above a few thousand lines.

A graph database over git history solves this by pre-computing relationships that agents can query in O(1) lookups:

- **Impact**: File X co-changes with 14 other files -- here they are, ranked by co-change frequency.

The graph lives locally (`.git-agent/graph.db`), is automatically maintained via EnsureIndex, and is queryable via flat CLI commands that return machine-parseable JSON (when piped) or human-readable text (in a terminal).

---

## 2. Requirements

### P0 -- Must ship in v1

| # | Requirement | Rationale |
|---|-------------|-----------|
| R01 | **Index git history into graph DB** -- parse commits, extract file modifications, author info, and store as Commit, File, Author nodes with MODIFIES and AUTHORED edges | Foundation for all queries |
| R02 | **Incremental indexing** -- track last indexed commit hash, only process new commits on subsequent runs | Repos with 10k+ commits must not re-index from scratch every time |
| R03 | **Co-change detection** -- compute CO_CHANGED edges between files that appear in the same commit, with a `count` weight | Core signal for impact analysis |
| R04 | **Impact query (co-change only)** -- given a file path, return co-changed files ranked by frequency | Primary use case from discovery |
| R05 | **TTY-aware output** -- text for terminal, JSON when piped; `--json`/`--text` flags override | Agents parse JSON; humans read text |
| R06 | **EnsureIndex (auto-indexing)** -- before `impact` queries and `commit` flow, automatically build or update the graph (DB missing -> full index; unreachable lastHash -> full re-index; else -> incremental) | No manual `index` command needed; transparent to user |
| R07 | **`git-agent impact <path>` command** -- executes the impact query | Primary agent-facing command |
| R08 | **Graph storage in `.git-agent/graph.db`** -- SQLite database file alongside existing `.git-agent/config.yml` | Consistent with existing config location; easy to gitignore |
| R09 | **`.gitignore` integration** -- EnsureIndex ensures `graph.db` is in `.git-agent/.gitignore` or the project `.gitignore` | Graph DB should never be committed |
| R10 | **Graceful handling of auto-index failure** -- if EnsureIndex fails, return a clear error (exit code 3) | Agents need actionable error messages |
| R11 | **Commit co-change enhancement** -- before `planner.Plan()`, if graph.db exists, query co-change for staged files and inject `CoChangeHints` into the planning prompt (invisible, zero new flags) | Improves commit grouping by making the planner aware of file relationships |

### P1b -- Action Capture + Timeline (can ship after v1)

| # | Requirement | Rationale |
|---|-------------|-----------|
| R19 | **`git-agent capture` command** -- record an agent or human action (diff + metadata) into the graph as a Session/Action node; command is hidden from help | Foundation for timeline and diagnose; called by agent hooks |
| R20 | **Session tracking** -- group sequential actions from the same source into sessions, with automatic timeout-based lifecycle | Provides context grouping for timeline display |
| R21 | **Agent hook integration** -- Claude Code `PostToolUse` hook calls `capture` after each `Edit`/`Write`/`Bash` tool call | Primary mechanism for capturing agent actions at tool-call granularity |
| R22 | **`git-agent timeline` command** -- display session/action history, filterable by time, source, and file | Human-readable history of what agents and humans did |
| R23 | **Action-to-file attribution** -- action_modifies rows link each action to the files it changed, with addition/deletion counts | Enables per-action impact analysis |
| R24 | **Action-to-commit linking** -- when `git-agent commit` runs, link preceding uncommitted actions to the resulting Commit node via action_produces | Bridges action-level and commit-level history |

### P2 -- Future / nice-to-have

| # | Requirement | Rationale |
|---|-------------|-----------|
| R25 | **LLM timeline compression** -- `timeline --compress` sends grouped actions to LLM and returns human-readable session summaries | Raw diffs are noisy; compressed timeline is what humans want to read |
| R26 | **`git-agent diagnose` command** -- P2 stub that prints "not yet implemented" and exits 0; future implementation combines impact + action timeline + LLM reasoning | Placeholder for future AI-enhanced `git bisect` at action granularity |
| R27 | **Coupling score between two paths** | Quantifies hidden dependencies |
| R28 | **Stability metrics for a path** | Identifies volatile vs stable code |
| R29 | **Time-windowed queries** -- `--since` and `--until` flags on query commands | Focus analysis on recent history |
| R30 | **Graph export** -- dump graph as DOT or Mermaid for visualization | Debugging and documentation |
| R31 | **Watch mode** -- auto-reindex on new commits | Zero-friction for long-running agent sessions |
| R32 | **MCP server mode** -- expose graph queries as MCP tools | Native integration for MCP-aware agents |

---

## 3. Success Criteria

### Functional

| Criterion | Measurement |
|-----------|------------|
| Index a 5,000-commit repo with <100 files | Completes in under 30 seconds on first run |
| Incremental index after 1 new commit | Completes in under 2 seconds |
| Impact query returns results | Response in under 500ms for a repo with 10k file nodes |
| Agent integration works end-to-end | Claude Code can call `git-agent impact src/main.go`, parse JSON output, and use it to inform a refactoring decision |
| EnsureIndex is transparent | Queries and commit flow work without manual indexing |
| Commit enhancement is invisible | Co-change hints improve commit grouping with zero new flags |
| All queries return valid JSON (when piped) | Output passes `jq .` validation; schema is stable across minor versions |

### Quality

| Criterion | Measurement |
|-----------|------------|
| Existing tests still pass | `make test` green after graph feature merge |
| Graph feature has unit + e2e tests | Coverage for all P0 requirements |
| Binary size increase is bounded | Total under 20MB (current ~7.2MB + modernc.org/sqlite ~8-12MB) |
| No regression in commit/init commands | Existing e2e tests pass unchanged |

---

## 4. Constraints

### C1: Pure Go SQLite (modernc.org/sqlite)

`modernc.org/sqlite` is a pure Go transpilation of SQLite. Zero CGo, full cross-compilation support. Implications:

- **Binary size**: adds ~8-12MB (the transpiled SQLite C code compiled to Go).
- **Performance**: ~1.1-2x slower than CGo-based `mattn/go-sqlite3`. Acceptable for a CLI tool indexing repos of typical size.
- **No platform-specific build steps**: `GOOS`/`GOARCH` work out of the box.
- **No build tags needed**: graph feature compiles unconditionally into every binary.

### C3: Clean Architecture compliance

The graph feature must follow the existing 4-layer structure:

```
cmd/impact.go             -- Cobra wiring, flag parsing, TTY-aware output
application/graph_*.go    -- Service orchestration
domain/graph/             -- Node, Edge, Relationship interfaces and value objects
infrastructure/graph/     -- SQLite adapter, git history walker
```

Domain must have zero external imports. SQLite lives exclusively in `infrastructure/`.

### C4: Existing dependency surface

The project currently has exactly 3 direct dependencies (go-openai, cobra, yaml.v3). Adding modernc.org/sqlite increases the dependency count. Each new dependency must be justified.

### C5: Storage budget

`.git-agent/graph.db` size target:
- A 10k-commit, 500-file repo should produce a graph DB under 50MB.
- A 1k-commit, 100-file repo should produce a graph DB under 10MB.

### C6: No network requirement

Graph indexing and querying must work entirely offline. No LLM calls. No API calls. Pure local computation over local git history. `timeline --compress` (P2) and `diagnose` (P2) are the only commands requiring network.

---

## 5. CLI UX Design

### Command tree

```
git-agent impact <path>     Show co-changed files (auto-indexes via EnsureIndex)
git-agent timeline          Show action history                                  (P1b)
git-agent capture           (hidden) Record an agent/human action                (P1b)
git-agent diagnose          (P2 stub) Prints "not yet implemented", exits 0      (P2)
```

All commands are flat on rootCmd. There is no `graph` parent command.

Users reset the graph by deleting the DB manually: `rm .git-agent/graph.db*`

### Command examples and agent usage patterns

#### `git-agent impact`

```bash
# Co-change impact (auto-indexes if needed)
$ git-agent impact src/application/commit_service.go
{
  "target": "src/application/commit_service.go",
  "co_changed": [
    {"path": "src/cmd/commit.go", "coupling_count": 42, "coupling_strength": 0.78},
    {"path": "src/application/commit_service_test.go", "coupling_count": 38, "coupling_strength": 0.70},
    {"path": "src/domain/commit/generator.go", "coupling_count": 15, "coupling_strength": 0.28}
  ],
  "total_co_changed": 12,
  "query_ms": 8
}

# Top N results only
$ git-agent impact src/main.go --top 5

# Minimum co-change threshold
$ git-agent impact src/main.go --min-count 3

# Force JSON output (even in terminal)
$ git-agent impact src/main.go --json

# Force text output (even when piped)
$ git-agent impact src/main.go --text
```

**Agent pattern**: Before modifying a file, run impact to understand downstream impact. Include affected files in the change plan.

#### `git-agent capture` (P1b, hidden)

```bash
# Called by Claude Code PostToolUse hook after an Edit tool call
$ git-agent capture --source claude-code --tool Edit
{"action_id":"s1:3","session_id":"s1","files_changed":["src/application/commit_service.go"],"capture_ms":45}

# No diff detected -- no-op
$ git-agent capture --source claude-code --tool Edit
{"action_id":null,"skipped":true,"reason":"no changes detected"}

# End a session explicitly
$ git-agent capture --source claude-code --end-session
{"session_id":"s1","ended":true,"total_actions":4}

# With a human-readable message
$ git-agent capture --source human --message "fixed auth middleware"
{"action_id":"s2:1","session_id":"s2","files_changed":["src/middleware/auth.go"]}
```

**Agent pattern**: Configured as a `PostToolUse` hook. Runs automatically after every `Edit`/`Write`/`Bash` call. The agent does not invoke this directly.

#### `git-agent timeline` (P1b)

```bash
# Raw timeline (offline, no LLM)
$ git-agent timeline --since 2h
{
  "sessions": [
    {
      "id": "s1",
      "source": "claude-code",
      "started_at": "2026-04-06T14:02:00Z",
      "ended_at": "2026-04-06T14:15:00Z",
      "actions": [
        {"id": "s1:1", "tool": "Edit", "timestamp": "2026-04-06T14:02:12Z", "files": ["src/application/commit_service.go"], "summary": null},
        {"id": "s1:2", "tool": "Write", "timestamp": "2026-04-06T14:03:45Z", "files": ["src/application/commit_service_test.go"], "summary": null}
      ],
      "summary": null
    }
  ],
  "total_sessions": 1,
  "total_actions": 2,
  "query_ms": 28
}

# Compressed timeline (requires LLM, P2)
$ git-agent timeline --since 2h --compress

# Filter by file
$ git-agent timeline --file src/application/commit_service.go

# Filter by source
$ git-agent timeline --source claude-code --since 1d
```

**Agent pattern**: Before starting work, review `timeline --since 1d --compress` to understand recent changes and avoid conflicts.

#### `git-agent diagnose` (P2 stub)

```bash
$ git-agent diagnose
not yet implemented
$ echo $?
0
```

### Global flags

All commands inherit:
- `--json` -- force JSON output
- `--text` -- force text output
- `--verbose` / `-v` -- detailed progress to stderr

Default: TTY-aware (text for terminal, JSON when piped).

### Exit codes

Consistent with existing conventions in `pkg/errors/`:
- `0` -- success
- `1` -- general error (no git repo, DB corruption, invalid arguments)
- `3` -- auto-index failure (EnsureIndex could not build or update the graph)

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

#### Impact result

```json
{
  "target": "src/application/commit_service.go",
  "co_changed": [
    {
      "path": "src/cmd/commit.go",
      "coupling_count": 42,
      "coupling_strength": 0.78
    }
  ],
  "total_co_changed": 12,
  "query_ms": 8
}
```

#### Capture result (P1b)

```json
{
  "action_id": "s1:3",
  "session_id": "s1",
  "files_changed": ["src/application/commit_service.go"],
  "capture_ms": 45
}
```

When no diff is detected: `{"action_id": null, "skipped": true, "reason": "no changes detected"}`.

#### Timeline result (P1b)

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

#### Error result

```json
{
  "error": "auto-index failed",
  "detail": "...",
  "exit_code": 3
}
```

---

## 7. Risk Register

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| T1 | **SQLite recursive CTE performance** -- complex graph traversals may be slower than a dedicated graph DB | Low | Medium | Per-repo graphs are small (thousands of nodes). Benchmark against real repos. Add depth limits to all recursive CTEs. |
| T2 | **SQLite binary size** -- modernc.org/sqlite adds ~8-12MB to the binary | Low | Low | Acceptable for a modern CLI tool. Total binary ~15-20MB. |
| T3 | **modernc.org/sqlite compatibility** -- transpiled SQLite may have edge-case differences from native SQLite | Low | Low | modernc.org/sqlite tracks upstream SQLite closely (v3.51.x). 3,400+ importers in production. |
| T4 | **Large repo indexing performance** -- repos with 50k+ commits may take minutes to index | Medium | Medium | Batch git log parsing (1000 commits at a time). Use SQLite transactions with batch inserts. Show progress bar on stderr. |
| T5 | **Co-change combinatorial explosion** -- a commit touching 100 files generates 4950 CO_CHANGED edges | Medium | Medium | Cap CO_CHANGED generation: skip commits touching more than 50 files (likely merge commits or bulk reformats). |
| T7 | **Graph DB corruption** -- crash during indexing leaves DB in inconsistent state | Low | Medium | Use SQLite transactions (WAL mode). `rm .git-agent/graph.db*` provides manual recovery. |
| T8 | **Concurrent access** -- multiple agent sessions or terminal tabs run graph commands simultaneously | Low | Medium | SQLite WAL mode supports concurrent reads with a single writer. Built-in busy timeout (5s). |
| T9 | **Scope creep into MCP/HTTP** -- temptation to add server mode before CLI is solid | Medium | Medium | Hard boundary: v1 is CLI-only (R32 is P2). |
| T10 | ~~**Breaking existing builds**~~ | -- | -- | Resolved: pure Go SQLite eliminates CGo. No build tags needed. |
| T11 | **`capture` latency blocks agent** -- if capture takes >200ms, it degrades agent UX as a PostToolUse hook | Medium | High | Capture must be fast: read `git diff`, write 2-3 rows, exit. No LLM calls. |
| T12 | **Diff storage bloat** -- storing full diffs for every action can grow the DB rapidly in long sessions | Medium | Medium | Truncate diffs over 100KB. Track action row count. |
| T13 | **Claude Code hook API changes** -- hook configuration format may evolve across Claude Code versions | Low | Medium | Keep hook setup in documentation, not hardcoded. |
| T14 | **LLM dependency for compress/diagnose** -- these commands fail without LLM access, breaking offline expectation | Medium | Low | Clearly document that `--compress` and `diagnose` (future) require LLM. All other commands remain fully offline. |
| T15 | **Session boundary heuristics** -- 30-minute timeout may not match real agent session boundaries | Low | Low | Make timeout configurable in `.git-agent/config.yml`. |
| T16 | **Capture diff accumulation** -- raw `git diff` includes changes from prior uncaptured tool calls, causing incorrect file attribution | High | High | Mitigated: delta-based capture via `capture_baseline` table. |
| T17 | **File rename breaks co-change continuity** -- renamed files lose their co-change history | Medium | Medium | Mitigated: `renames` table tracks `old_path -> new_path` per commit. |
| T18 | **History rewrite invalidates index state** -- force-push or rebase makes `last_indexed_commit` unreachable | Medium | High | Mitigated: EnsureIndex detects via `merge-base --is-ancestor` and falls back to full re-index. |
| T19 | **Schema migration on upgrade** -- new columns or tables in future versions cause errors | Low | Medium | Store `schema_version` in `index_state`. Auto-migrate on open. |
| T20 | **Concurrent same-source sessions** -- two Claude Code instances writing to the same session | Medium | Medium | Mitigated: `instance_id` field on sessions (defaults to `$PPID`). |

---

## Appendix: Graph Schema (SQLite DDL)

> **Authoritative schema**: See [_index.md](./_index.md) for the complete DDL
> with all 13 tables, indexes, and constraints. This appendix is a simplified overview.

```sql
-- Node tables
CREATE TABLE commits (hash TEXT PRIMARY KEY, message TEXT, author_name TEXT, author_email TEXT, timestamp INTEGER, parent_hashes TEXT);
CREATE TABLE files (path TEXT PRIMARY KEY, language TEXT, last_indexed_hash TEXT);
CREATE TABLE authors (email TEXT PRIMARY KEY, name TEXT);
CREATE TABLE index_state (key TEXT PRIMARY KEY, value TEXT);

-- Edge tables (relationships)
CREATE TABLE modifies (commit_hash TEXT, file_path TEXT, additions INTEGER, deletions INTEGER, status TEXT, PRIMARY KEY (commit_hash, file_path));
CREATE TABLE authored (author_email TEXT, commit_hash TEXT, PRIMARY KEY (author_email, commit_hash));
CREATE TABLE co_changed (file_a TEXT, file_b TEXT, coupling_count INTEGER, coupling_strength REAL, last_coupled_hash TEXT, PRIMARY KEY (file_a, file_b), CHECK (file_a < file_b));
CREATE TABLE renames (old_path TEXT, new_path TEXT, commit_hash TEXT, PRIMARY KEY (old_path, new_path, commit_hash));

-- Session/Action tables (P1b)
CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, instance_id TEXT, started_at INTEGER, ended_at INTEGER, summary TEXT);
CREATE TABLE actions (id TEXT PRIMARY KEY, session_id TEXT, sequence INTEGER, tool TEXT, diff TEXT, files_changed TEXT, timestamp INTEGER, message TEXT, summary TEXT);
CREATE TABLE action_modifies (action_id TEXT, file_path TEXT, additions INTEGER, deletions INTEGER, PRIMARY KEY (action_id, file_path));
CREATE TABLE action_produces (action_id TEXT, commit_hash TEXT, PRIMARY KEY (action_id, commit_hash));
CREATE TABLE capture_baseline (file_path TEXT PRIMARY KEY, content_hash TEXT, captured_at INTEGER);
```

`co_changed.coupling_strength` = coupling_count / max(commits_file_a, commits_file_b). The `CHECK (file_a < file_b)` constraint ensures canonical ordering (each pair stored once).

`modifies.status` mirrors git's status codes: `A` (added), `M` (modified), `D` (deleted), `R` (renamed).
