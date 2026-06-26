# git-agent graph: Code Knowledge Graph for Coding Agents

## Context

Coding agents (Claude Code, Cursor, Windsurf) lack structural awareness of codebases.
When an agent asks "what will break if I change this function?", today's options are
inadequate: flat `git log` (no relationships), grep (misses semantics), or reading the
whole codebase (context window limits).

Beyond structural awareness, agents also lack **behavioral traceability**. When a bug
surfaces, there is no way to answer "which agent action introduced this regression?"
because agent edits (each `Edit`, `Write`, `Bash` tool call) are invisible -- they
collapse into a single git commit, losing the fine-grained action history.

A pre-computed graph stored in SQLite over git history + agent/human action timeline
enables fast relationship lookups and behavioral tracing that agents can query via CLI
commands returning machine-parseable JSON.

### Q&A History

- **Technology choice**: SQLite via `modernc.org/sqlite` (pure Go, zero CGo)
- **Priority**: Co-change impact analysis first (primary agent need)
- **Scope**: P0 = git history + co-change + impact + EnsureIndex + commit enhancement; P1b = action capture + timeline + diagnose stub + hooks; P2 = diagnose implementation + LLM compression + advanced metrics
- **Action capture**: Agent hooks (Claude Code `PostToolUse`) feed diffs into graph via `capture`
- **Timeline**: LLM-compressed session summaries; raw mode available offline
- **Diagnose**: P2 stub -- prints "not yet implemented", exits 0
- **Build**: Always-on in single binary, no build tags, no CGo
- **Auto-indexing**: EnsureIndex runs automatically before `impact` queries and `commit` flow; no manual `index` command
- **Flat commands**: All graph commands are top-level on rootCmd (`impact`, `timeline`, `capture`, `diagnose`), no `graph` parent command
- **TTY-aware output**: Text for terminal, JSON when piped; `--json`/`--text` flags override
- **Commit enhancement**: Before `planner.Plan()`, if graph.db exists, query co-change for staged files and inject CoChangeHints into planning prompt (invisible, zero new flags)

## Discovery Results

### Codebase Analysis

- Current binary: 7.2MB, 3 direct dependencies (go-openai, cobra, yaml.v3), zero CGo
- Clean Architecture: `cmd -> application -> domain <- infrastructure`
- Domain has zero external imports; SQLite must live in `infrastructure/`
- Git operations abstracted via `infrastructure/git/client.go`
- Config follows 3-tier hierarchy: CLI flags > user config > project config > defaults

### Technology Research

- **KuzuDB** (original choice): archived Oct 2025, no maintained forks. Go binding
  `go-kuzu` v0.11.3 is also archived. CGo requirement would add 20-30MB and break
  cross-compilation. **Rejected.**
- **SQLite via `modernc.org/sqlite`** (v1.48.1): pure Go transpilation, zero CGo,
  full cross-compilation, +8-12MB binary size. 3,400+ importers, production-grade.
  Recursive CTEs handle graph traversal patterns adequately for per-repo scale
  (thousands of nodes, tens of thousands of edges).

### Technology Comparison

| Criteria | KuzuDB (rejected) | SQLite (chosen) |
|----------|-------------------|-----------------|
| CGo | Required | None (pure Go) |
| Binary impact | +20-30MB | +8-12MB |
| Cross-compilation | Broken by CGo | Works natively |
| Upstream status | Archived Oct 2025 | Active, 3,400+ importers |
| Query language | Cypher (elegant) | SQL + recursive CTEs (verbose but adequate) |
| Graph scale fit | Overkill for per-repo | Right-sized |
| Build complexity | Build tags, separate CI | Single build, single CI job |

## Requirements

**P0 (v1)**: Core requirements covering git history indexing, EnsureIndex
(auto-indexing), incremental updates, co-change detection, impact query
(co-change only), commit co-change enhancement, TTY-aware output, storage,
gitignore integration, error handling.

**P1b (post-v1, behavioral traceability)**: 6 requirements covering action
capture via agent hooks, session tracking, timeline display, action-to-file
attribution, action-to-commit linking, agent hook integration (Claude Code).

**P2 (future)**: Requirements covering diagnose implementation, LLM timeline
compression, coupling scores, stability metrics, time windows, graph export,
watch mode, MCP server mode.

See [Requirements](./requirements.md) for the full requirements document with success
criteria, constraints, CLI UX design, output format specification, and risk register.

### Traceability Matrix

| Req | Description | Design Section | BDD Scenario |
|-----|-------------|---------------|--------------|
| R01 | Index git history into graph DB | Schema DDL, Index Algorithm | First-time full index |
| R02 | Incremental indexing | Key Decision #3, IndexState | Incremental index after new commits, Idempotent |
| R03 | Co-change detection | co_changed table, Key Decision #2 | CO_CHANGED edges computed |
| R04 | Impact query (co-change only) | Impact SQL, Data Flow | Impact single file, JSON output |
| R05 | TTY-aware output with `--json`/`--text` override | Exit Codes, Subcommand Flags | Agent queries via CLI (JSON) |
| R06 | EnsureIndex (auto-indexing before queries and commit) | EnsureIndex Flow, Key Decision | EnsureIndex scenarios |
| R07 | `impact <path>` command | CLI Command Tree, Data Flow | Impact scenarios |
| R08 | Storage in `.git-agent/graph.db` | Storage | First-time full index |
| R09 | Gitignore integration | Storage | Auto-adds graph.db to gitignore |
| R10 | Graceful empty/missing graph (auto-index fallback) | Exit Codes | EnsureIndex creates DB if missing |
| R11 | Commit co-change enhancement | Commit Enhancement Flow | Commit enhancement scenarios |
| R19 | Action capture via `capture` | Session/Action schema, Hook Integration | Capture agent edit action |
| R20 | Session tracking (group actions) | Session schema, Data Flow | Session lifecycle |
| R21 | Agent hook integration (Claude Code) | Hook Integration | Claude Code PostToolUse hook triggers capture |
| R22 | `timeline` command | CLI Command Tree, Data Flow | Timeline raw, Timeline filtered |
| R23 | Action-to-file attribution | action_modifies schema | Action modifies tracked files |
| R24 | Action-to-commit linking | action_produces schema | Actions linked to resulting commit (via git-agent commit) |
| R25 | LLM timeline compression (P2) | Key Decision #8, Data Flow | Timeline compressed |
| R26 | `diagnose` command (P2 stub) | CLI Command Tree | Diagnose prints "not yet implemented" |
| R27 | Coupling score (P2) | co_changed table | -- |
| R28 | Stability metrics (P2) | -- | -- |
| R29 | Time-windowed queries on commands (P2) | Subcommand Flags (--since/--until) | -- |
| R30 | Graph export (P2) | -- | -- |
| R31 | Watch mode (P2) | -- | -- |
| R32 | MCP server mode (P2) | -- | -- |

## Rationale

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Graph DB | SQLite via `modernc.org/sqlite` | Pure Go, zero CGo, cross-compiles, +8-12MB, active upstream |
| Schema | Relational tables modeling a property graph | Natural keys enable idempotent INSERT OR REPLACE |
| Indexing | EnsureIndex (auto-indexing: DB missing -> full; unreachable lastHash -> full re-index; else -> incremental) | No manual `index` command; transparent to user |
| Interface | Flat CLI commands (`git-agent impact ...`) | Agents call via shell, parse JSON stdout; flat is simpler than nested `graph` namespace |
| Build | Always-on, single binary, no build tags | SQLite is pure Go; no reason to split builds |
| Priority | P0 co-change + impact + commit enhancement, P1b actions, P2 diagnose | Ship co-change awareness first, behavioral tracing second |
| Action capture | Delta-based via agent hooks (PostToolUse) | capture_baseline prevents diff accumulation across tool calls |
| Timeline | LLM compression optional, raw mode offline | Keeps offline default; LLM enrichment is opt-in |
| Diagnose | P2 stub -- prints "not yet implemented", exits 0 | Deferred until action capture is mature |
| History rewrite | Auto-detect via merge-base, fall back to full re-index | Prevents silent corruption from force-push/rebase |
| Rename tracking | `renames` table populated during indexing | Preserves co-change continuity across file renames |
| Schema migration | Version key in index_state, forward migrations | Avoids unnecessary reset on minor upgrades |
| Commit enhancement | Query co-change for staged files, inject CoChangeHints into planner prompt | Invisible to user, zero new flags, improves commit grouping |
| Output format | TTY-aware default (text for terminal, JSON when piped), `--json`/`--text` override | Natural UX for both humans and agents |

### Alternatives Considered

1. **KuzuDB embedded (`go-kuzu` v0.11.3)**: Cypher-native property graph, elegant
   query language. Rejected: archived Oct 2025, requires CGo (+20-30MB), breaks
   cross-compilation, no maintained forks.
2. **bbolt + in-memory graph**: +0.6MB, minimal binary impact. Rejected: no query
   language limits future flexibility for ad-hoc graph queries.
3. **Cayley**: Go graph DB but requires CGo via mattn/go-sqlite3, 141 total
   dependencies. Rejected: worse than SQLite on all relevant metrics.
4. **No DB, JSON/CSV**: Simplest storage. Rejected: doesn't scale for incremental
   updates and ad-hoc queries.

## Detailed Design

### SQLite Schema

```sql
-- Core node tables
CREATE TABLE IF NOT EXISTS commits (
    hash TEXT PRIMARY KEY,
    message TEXT,
    author_name TEXT,
    author_email TEXT,
    timestamp INTEGER,
    parent_hashes TEXT  -- JSON array: '["abc123","def456"]'
);

CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    language TEXT,
    last_indexed_hash TEXT
);

CREATE TABLE IF NOT EXISTS authors (
    email TEXT PRIMARY KEY,
    name TEXT
);

-- Edge tables (relationships)
CREATE TABLE IF NOT EXISTS authored (
    author_email TEXT NOT NULL,
    commit_hash TEXT NOT NULL,
    PRIMARY KEY (author_email, commit_hash),
    FOREIGN KEY (author_email) REFERENCES authors(email),
    FOREIGN KEY (commit_hash) REFERENCES commits(hash)
);

CREATE TABLE IF NOT EXISTS modifies (
    commit_hash TEXT NOT NULL,
    file_path TEXT NOT NULL,
    additions INTEGER DEFAULT 0,
    deletions INTEGER DEFAULT 0,
    status TEXT,          -- A (added), M (modified), D (deleted), R (renamed)
    PRIMARY KEY (commit_hash, file_path),
    FOREIGN KEY (commit_hash) REFERENCES commits(hash),
    FOREIGN KEY (file_path) REFERENCES files(path)
);

CREATE TABLE IF NOT EXISTS co_changed (
    file_a TEXT NOT NULL,
    file_b TEXT NOT NULL,
    coupling_count INTEGER DEFAULT 0,
    coupling_strength REAL DEFAULT 0.0,
    last_coupled_hash TEXT,
    PRIMARY KEY (file_a, file_b),
    FOREIGN KEY (file_a) REFERENCES files(path),
    FOREIGN KEY (file_b) REFERENCES files(path),
    CHECK (file_a < file_b)  -- canonical ordering prevents duplicates
);

-- File rename tracking
CREATE TABLE IF NOT EXISTS renames (
    old_path TEXT NOT NULL,
    new_path TEXT NOT NULL,
    commit_hash TEXT NOT NULL,
    PRIMARY KEY (old_path, new_path, commit_hash),
    FOREIGN KEY (commit_hash) REFERENCES commits(hash)
);

-- Index state (metadata)
CREATE TABLE IF NOT EXISTS index_state (
    key TEXT PRIMARY KEY,
    value TEXT
);

-- Session/Action tables (P1b)
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,          -- UUID
    source TEXT NOT NULL,         -- "claude-code", "cursor", "windsurf", "human"
    instance_id TEXT,             -- distinguishes concurrent agents of same source (e.g., PID)
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    summary TEXT                  -- LLM-compressed (nullable, filled by timeline --compress)
);

CREATE TABLE IF NOT EXISTS actions (
    id TEXT PRIMARY KEY,          -- "{session_id}:{sequence_number}"
    session_id TEXT NOT NULL,
    sequence INTEGER NOT NULL DEFAULT 0,
    tool TEXT,                    -- "Edit", "Write", "Bash", "manual-save", null
    diff TEXT,                    -- unified diff (truncated at 100KB)
    files_changed TEXT,           -- JSON array: '["src/main.go"]'
    timestamp INTEGER NOT NULL,
    message TEXT,
    summary TEXT,                 -- LLM-compressed (nullable)
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS action_modifies (
    action_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    additions INTEGER DEFAULT 0,
    deletions INTEGER DEFAULT 0,
    PRIMARY KEY (action_id, file_path),
    FOREIGN KEY (action_id) REFERENCES actions(id),
    FOREIGN KEY (file_path) REFERENCES files(path)
);

CREATE TABLE IF NOT EXISTS action_produces (
    action_id TEXT NOT NULL,
    commit_hash TEXT NOT NULL,
    PRIMARY KEY (action_id, commit_hash),
    FOREIGN KEY (action_id) REFERENCES actions(id),
    FOREIGN KEY (commit_hash) REFERENCES commits(hash)
);

-- Capture baseline tracking (delta-based action capture)
CREATE TABLE IF NOT EXISTS capture_baseline (
    file_path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,    -- git hash-object of file at last capture
    captured_at INTEGER NOT NULL
);

-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_commits_timestamp ON commits(timestamp);
CREATE INDEX IF NOT EXISTS idx_modifies_file ON modifies(file_path);
CREATE INDEX IF NOT EXISTS idx_modifies_commit ON modifies(commit_hash);
CREATE INDEX IF NOT EXISTS idx_co_changed_file_a ON co_changed(file_a);
CREATE INDEX IF NOT EXISTS idx_co_changed_file_b ON co_changed(file_b);
CREATE INDEX IF NOT EXISTS idx_co_changed_strength ON co_changed(coupling_strength);
CREATE INDEX IF NOT EXISTS idx_actions_session ON actions(session_id);
CREATE INDEX IF NOT EXISTS idx_actions_timestamp ON actions(timestamp);
CREATE INDEX IF NOT EXISTS idx_action_modifies_file ON action_modifies(file_path);
CREATE INDEX IF NOT EXISTS idx_sessions_source_instance ON sessions(source, instance_id);
CREATE INDEX IF NOT EXISTS idx_renames_old ON renames(old_path);
CREATE INDEX IF NOT EXISTS idx_renames_new ON renames(new_path);
```

### CLI Command Tree

```
git-agent impact <path>     Show co-changed files (auto-indexes via EnsureIndex)
git-agent timeline          Show action history                                  (P1b)
git-agent capture           (hidden) Record an agent/human action                (P1b)
git-agent diagnose          (P2 stub) Prints "not yet implemented", exits 0      (P2)
```

All commands are flat on rootCmd. There is no `graph` parent command.

### Subcommand Flags

| Command | Flags | Default |
|---------|-------|---------|
| `impact` | `PATH` (positional), `--top N`, `--min-count N` | required, 20, 3 |
| `capture` | `--source NAME`, `--tool NAME`, `--session ID`, `--instance-id ID`, `--message TEXT` | required, null, auto-create, $PPID, null |
| `timeline` | `--since DATE\|DURATION`, `--until DATE`, `--source NAME`, `--file PATH`, `--compress`, `--top N` | all, now, all, all, false, 50 |
| `diagnose` | (P2 stub, no flags) | -- |
| All | `--json`, `--text`, `--verbose` | TTY-aware (text for terminal, JSON when piped) |

### Exit Codes

- `0` -- success
- `1` -- general error
- `2` -- hook blocked commit (existing)
- `3` -- auto-index failure (EnsureIndex could not build or update the graph)

### Storage

- Location: `.git-agent/graph.db` (single SQLite file)
- Metadata: `index_state` table inside the database
- Gitignore: EnsureIndex auto-adds `graph.db` to `.git-agent/.gitignore`
- Size targets: <10MB for 1k-commit repo, <50MB for 10k-commit repo
- Concurrency: WAL mode enables concurrent reads during writes
- Reset: Users delete the DB manually via `rm .git-agent/graph.db*`

### Data Flow

```mermaid
graph TD
    subgraph "EnsureIndex (auto-indexing)"
        EI1[Query or commit triggered] --> EI2{graph.db exists?}
        EI2 -->|No| EI3[Full index from scratch]
        EI2 -->|Yes| EI4{lastHash reachable from HEAD?}
        EI4 -->|No: force-push/rebase| EI5[Full re-index]
        EI4 -->|Yes| EI6[Incremental: git log lastHash..HEAD]
        EI3 --> EI7[Index commits, files, authors, co_changed]
        EI5 --> EI7
        EI6 --> EI7
        EI7 --> EI8[UPDATE index_state]
    end

    subgraph "Index Algorithm (shared by EnsureIndex)"
        A[git log range] --> D{For each commit}
        D --> E[INSERT OR IGNORE Commit + Author]
        D --> F[INSERT modifies edges]
        D --> F2{Rename detected?}
        F2 -->|Yes| F3[INSERT renames row]
        F2 -->|No| G
        F3 --> G
        D --> L[Incremental co_changed update for touched files]
        L --> M[UPDATE index_state + schema_version]
    end

    subgraph "git-agent impact <path>"
        N[CLI args: file path] --> N2[EnsureIndex]
        N2 --> N3[Resolve renames: union old+new paths]
        N3 --> O[GraphImpactService.Impact]
        O --> P[SQL: co_changed neighbors]
        P --> S[Rank + deduplicate results]
        S --> T[Output: text or JSON based on TTY]
    end

    subgraph "Commit co-change enhancement"
        CE1[git-agent commit] --> CE2{graph.db exists?}
        CE2 -->|No| CE3[Skip enhancement, proceed normally]
        CE2 -->|Yes| CE4[EnsureIndex]
        CE4 --> CE5[Query co-change for staged files]
        CE5 --> CE6[Build CoChangeHints]
        CE6 --> CE7[Inject into planner.Plan prompt]
        CE7 --> CE8[Normal commit flow continues]
    end

    subgraph "git-agent capture (P1b, called by agent hook)"
        CA[Hook trigger] --> CB[CaptureService.Capture]
        CB --> CB2[git diff --name-only: changed files]
        CB2 --> CB3[Load capture_baseline hashes]
        CB3 --> CB4[Compute delta: hash-object vs baseline]
        CB4 --> CB5{Delta files exist?}
        CB5 -->|No| CI2[Return skipped]
        CB5 -->|Yes| CC{Session exists for source+instance?}
        CC -->|No| CD[INSERT session]
        CC -->|Yes| CE[Reuse session]
        CD --> CF[git diff only delta files]
        CE --> CF
        CF --> CG[INSERT action with delta diff]
        CG --> CH[INSERT action_modifies for delta files only]
        CH --> CJ[UPDATE capture_baseline hashes]
        CJ --> CI[Return action ID]
    end

    subgraph "git-agent timeline (P1b)"
        TA[CLI args: filters] --> TB[CaptureService.Timeline]
        TB --> TC[SQL: sessions + actions in range]
        TC --> TD{--compress flag?}
        TD -->|No| TE[Return raw action list]
        TD -->|Yes| TF[Group actions by session]
        TF --> TG[LLM: compress each session into summary]
        TG --> TH[Return compressed timeline]
    end
```

### Key Design Decisions

1. **Natural keys over synthetic IDs**: Commit hash, file path enable idempotent
   INSERT OR REPLACE / INSERT OR IGNORE operations. Re-indexing the same commit
   is a no-op.

2. **co_changed canonical ordering**: `CHECK (file_a < file_b)` constraint ensures
   each pair is stored once. Coupling strength = co_occurrences /
   max(individual_commits_a, individual_commits_b). Minimum threshold of 3
   co-changes filters noise from bulk reformats.

3. **Hybrid incremental strategy**:
   - Commits, Authors, authored, modifies: INSERT OR IGNORE (append-only)
   - co_changed: Incremental update -- recompute only pairs involving files
     modified in newly indexed commits. Full recompute on force re-index or when
     newly indexed commits exceed 500 (threshold configurable).

4. **Always-on in single binary**: SQLite via `modernc.org/sqlite` is pure Go.
   No CGo, no build tag isolation, no separate build targets. Commands are always
   available. Binary size increase (~8-12MB for SQLite) is acceptable for a
   modern CLI tool.

5. **SQLite tuning for CLI**:
   - WAL journal mode (concurrent reads during writes)
   - `synchronous=NORMAL` (safe for a local cache that can be rebuilt)
   - 64MB page cache, 256MB mmap
   - Read-only mode for query commands

6. **Batch inserts via transactions**: Wrap each indexing batch in
   `BEGIN`/`COMMIT`. Prepared statements with parameter binding. Expected
   throughput: 50-100K inserts/second.

7. **Action capture is hook-driven, not polling**: Agent hooks (Claude Code
   `PostToolUse`) call `git-agent capture` with the current `git diff`.
   This gives precise per-tool-call attribution. Human edits fall back to
   commit-level granularity.

8. **Timeline has two modes: raw (offline) and compressed (LLM)**:
   `timeline` without `--compress` returns raw actions -- fully offline.
   `--compress` calls the configured LLM endpoint to produce human-readable
   summaries. Offline-first principle preserved.

9. **EnsureIndex (auto-indexing)**: No manual `index` command. Before `impact`
   queries and `commit` flow, EnsureIndex runs automatically:
   - DB missing -> full index from scratch
   - lastHash unreachable from HEAD (force-push/rebase) -> full re-index
   - Otherwise -> incremental (git log lastHash..HEAD)
   This removes the need for users/agents to remember to run indexing.

10. **Session lifecycle is implicit**: A session starts on the first `capture`
    call with no active session. Ends after 30 minutes of inactivity, or
    explicitly via `capture --end-session`. Sessions are scoped by
    `source + instance_id` so concurrent agents of the same type maintain
    separate sessions. `instance_id` defaults to `$PPID`.

11. **Delta-based action capture**: Each `capture` call tracks file content
    hashes in a `capture_baseline` table. Only files whose `git hash-object`
    hash differs from the baseline are attributed to the new action. This
    prevents diff accumulation where later captures would incorrectly include
    changes from prior tool calls that were not yet staged or committed.

12. **Force-push / history rewrite recovery**: Before incremental indexing,
    verify `last_indexed_commit` is reachable from HEAD via
    `git merge-base --is-ancestor`. If not, automatically fall back to full
    re-index.

13. **File rename tracking**: Git rename status (`R`) is parsed into a
    `renames` table mapping `old_path -> new_path -> commit_hash`.
    Impact queries union results from all historical paths via the renames
    table, preserving co-change history across renames.

14. **Schema versioning**: The `index_state` table stores a `schema_version`
    key. On `Open`, the client checks the stored version against the current
    code version. If the version is older, the client runs forward migrations.
    If migration is not possible (major version bump), the client returns an
    error suggesting the user delete and re-create the DB.

15. **Flat commands on rootCmd**: All graph-related commands (`impact`,
    `timeline`, `capture`, `diagnose`) are registered directly on rootCmd,
    not under a `graph` parent command. This simplifies agent invocation and
    reduces typing.

16. **TTY-aware output**: Default output format is determined by whether stdout
    is a terminal. Terminal -> human-readable text. Piped/redirected -> JSON.
    `--json` and `--text` flags override the default.

17. **Commit co-change enhancement**: Before `planner.Plan()` in the commit
    flow, if graph.db exists, query co-change for staged files and inject
    `CoChangeHints` into the planning prompt. This is invisible to the user
    (zero new flags) and improves commit grouping by making the planner aware
    of file relationships.

### Constraints

- **C1**: `modernc.org/sqlite` adds ~8-12MB to binary. Acceptable.
- **C3**: Must follow existing Clean Architecture (domain has zero external imports).
- **C4**: Binary size target: total under 20MB (current ~7.2MB + SQLite ~8-12MB).
- **C5**: Storage target: <50MB for 10k-commit repos.
- **C6**: Offline by default. `impact`, `capture`, `timeline` (raw) require no
  network. `timeline --compress` and `diagnose` (when implemented) require LLM
  access.
- **C7**: Action capture must be fast (<200ms). Minimal work: read diff, write rows, exit.
- **C8**: Diff storage: TEXT column in SQLite. Diffs exceeding 100KB are truncated.
- **C9**: Capture must use delta-based tracking (capture_baseline table) to
  attribute only new changes per action.

### Performance Targets

| Operation | Target |
|-----------|--------|
| First index, 5k commits | <30 seconds |
| Incremental index, 1 new commit | <2 seconds |
| Impact query | <500ms |
| `capture` (single action) | <200ms |
| `timeline` (raw, 100 actions) | <300ms |
| `timeline --compress` (100 actions) | <10 seconds (LLM-bound) |

### Success Criteria

- Agent can call `git-agent impact src/main.go`, parse JSON, and use it
- EnsureIndex transparently builds/updates the graph before queries
- Commit flow uses co-change hints to improve grouping (invisible to user)
- Claude Code `PostToolUse` hook calls `capture`, actions appear in `timeline`
- `diagnose` prints "not yet implemented" and exits 0
- `make test` passes after merge (existing tests unaffected)
- All P0 requirements covered by unit + e2e tests
- Single binary, single build, zero CGo

### Hook Integration (P1b)

Agent hooks are the primary mechanism for feeding action data into the graph.

**Claude Code** (`~/.claude/settings.json`):
```jsonc
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": { "tool_name": "Edit|Write|Bash" },
        "command": "git-agent capture --source claude-code --tool $CLAUDE_TOOL_NAME --instance-id $PPID"
      }
    ]
  }
}
```

The `capture` command uses **delta-based tracking** to attribute only new
changes to each action, avoiding diff accumulation across tool calls:
1. Lists changed files via `git diff --name-only` (unstaged + staged)
2. For each changed file, computes content hash:
   - File exists on disk: `git hash-object <file>`
   - File deleted (in diff but missing from disk): sentinel hash `"deleted"`
3. Loads previous hashes from `capture_baseline` table
4. Computes delta: files whose hash differs from baseline (or absent from baseline)
5. If no delta files exist, exits 0 immediately (no-op)
6. Generates diff for delta files only: `git diff -- <delta files>`
7. Creates or reuses a Session row (keyed by source + instance_id + timeout)
8. Creates an Action row with the delta diff, tool name, and timestamp
9. Creates action_modifies rows for delta files only
10. Updates capture_baseline with current hashes for all changed files
11. Exits 0 (must never block the agent)

**Other agents**: Cursor and Windsurf lack native hook systems. Integration
options (P2):
- VS Code extension `onDidSaveTextDocument` callback
- Git `post-commit` hook for commit-level capture (coarser but universal)

## Design Documents

- [BDD Specifications](./bdd-specs.md) -- Behavior scenarios and testing strategy
- [Architecture](./architecture.md) -- System architecture and component details
- [Best Practices](./best-practices.md) -- SQLite and performance guidelines
- [Requirements](./requirements.md) -- Full context, requirements, CLI UX, risk register
