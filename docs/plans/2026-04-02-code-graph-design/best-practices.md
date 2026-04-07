# Best Practices: git-agent graph

## SQLite (modernc.org/sqlite)

### Schema Design

- **Relational tables model the property graph**: Node tables (`commits`,
  `files`, `symbols`, `authors`) and join tables for edges (`authored`,
  `modifies`, `co_changed`, `calls`, `imports`, `contains_symbol`).
- **Natural keys over surrogates**: Use commit hash, file path, composite
  symbol ID as primary keys. Enables `INSERT OR IGNORE` / `INSERT OR REPLACE`
  for idempotent operations -- re-indexing the same commit is a no-op.
- **Foreign keys for referential integrity**: Enable with
  `PRAGMA foreign_keys = ON` at connection open.
- **`IF NOT EXISTS`**: All `CREATE TABLE` statements use `IF NOT EXISTS` for
  safe schema initialization on every startup.

### Performance Tuning

| Setting | Value | Rationale |
|---------|-------|-----------|
| `PRAGMA journal_mode=WAL` | WAL | Concurrent reads during writes |
| `PRAGMA synchronous=NORMAL` | NORMAL | Faster writes, acceptable for local cache DB |
| `PRAGMA cache_size=-64000` | 64 MB page cache | Keeps hot pages in memory |
| `PRAGMA mmap_size=268435456` | 256 MB memory-mapped I/O | Reduces syscall overhead for large scans |
| `PRAGMA temp_store=MEMORY` | MEMORY | Temp tables and indexes stay in RAM |
| `PRAGMA busy_timeout=5000` | 5 seconds | Retry on lock contention before failing |
| `PRAGMA query_only=ON` | (read-only commands) | Prevents accidental writes in query paths |

All PRAGMAs are set once at connection open, before any queries.

### Batch Loading

- **Use explicit transactions**: `BEGIN` / many `INSERT` / `COMMIT`. Auto-commit
  per INSERT is 10-50x slower due to journal sync overhead.
- **Prepared statements with parameter binding**: Prepare once, execute many
  times with different parameter values. Avoids repeated query parsing.
- **For initial full index**: A single large transaction with prepared statements
  is fast enough. No need for bulk `COPY FROM` or external tooling.
- **Benchmark expectation**: ~50-100K inserts/second with prepared statements
  inside a transaction on typical developer hardware.

### Incremental Strategy

| Data type | Strategy | Reason |
|-----------|----------|--------|
| Commits, Authors | `INSERT OR IGNORE` | Append-only; duplicate insert is a no-op |
| MODIFIES, AUTHORED | `INSERT OR IGNORE` | Edge follows commit lifecycle |
| Symbols | `DELETE` by file_path + `INSERT` | AST must be fully reparsed; diffing individual symbols is fragile |
| CONTAINS, CALLS | `DELETE` by file_path + `INSERT` | Follow symbol lifecycle |
| IMPORTS | `DELETE` by file_path + `INSERT` | Follow file parse lifecycle |
| CO_CHANGED | Incremental: delete pairs involving touched files + recompute those pairs; full recompute on --force or >500 new commits | Avoids O(n^2) self-join on entire modifies table for small incremental updates |

### Query Patterns for Graph Traversal

- **Co-change pairs**: Simple `JOIN` on the `modifies` table, grouping by
  commit to find files that changed together.
- **Blast radius (co-change)**: Direct `SELECT` on `co_changed` filtered by
  `coupling_strength` threshold.
- **Blast radius (call chain)**: Recursive CTE with depth tracking and cycle
  prevention:
  ```sql
  WITH RECURSIVE call_chain(symbol_id, file_path, depth, visited) AS (
      SELECT s.id, s.file_path, 0, s.id
      FROM symbols s WHERE s.file_path = ?1

      UNION ALL

      SELECT c.to_symbol, s2.file_path, cc.depth + 1,
             cc.visited || '|' || c.to_symbol
      FROM call_chain cc
      JOIN calls c ON c.from_symbol = cc.symbol_id
      JOIN symbols s2 ON s2.id = c.to_symbol
      WHERE cc.depth < ?2
        AND instr('|' || cc.visited || '|', '|' || c.to_symbol || '|') = 0
  )
  SELECT DISTINCT file_path FROM call_chain WHERE depth > 0;
  ```
  Always include cycle prevention via delimiter-bounded `instr()` matching
  on the visited path. Do NOT use `LIKE '%' || symbol || '%'` -- it
  produces false positives when one symbol ID is a substring of another
  (e.g., `pkg/foo.go:function:Get:10` matching `pkg/foo.go:function:GetAll:10`).
  The `|` delimiter with `instr` ensures exact-match semantics.
- **Import reverse lookup**: Simple `SELECT from_file FROM imports WHERE to_file = ?`.
- **Hotspots**: `GROUP BY file_path` on `modifies` with `COUNT(*)` ordered
  descending.
- **Ownership**: `GROUP BY author_email` on `authored JOIN modifies` with
  `COUNT(*)` per file.

### CO_CHANGED Computation

Formula (CodeScene methodology):
```
coupling_strength = coupling_count / max(commits_file_a, commits_file_b)
```

Thresholds:
- Minimum co-change count: 3 (filters noise from bulk reformats)
- Maximum files per commit: 50 (skip merge commits and bulk operations)
- Both thresholds are configurable via `--min-count` and `--max-files-per-commit`

Computed as a single SQL statement:
```sql
INSERT INTO co_changed (file_a, file_b, coupling_count, coupling_strength)
WITH file_commit_counts AS (
    SELECT file_path, COUNT(DISTINCT commit_hash) AS total
    FROM modifies GROUP BY file_path
)
SELECT
    m1.file_path AS file_a,
    m2.file_path AS file_b,
    COUNT(DISTINCT m1.commit_hash) AS coupling_count,
    CAST(COUNT(DISTINCT m1.commit_hash) AS REAL)
      / MAX(fc1.total, fc2.total) AS coupling_strength
FROM modifies m1
JOIN modifies m2
  ON m1.commit_hash = m2.commit_hash
  AND m1.file_path < m2.file_path
JOIN file_commit_counts fc1 ON fc1.file_path = m1.file_path
JOIN file_commit_counts fc2 ON fc2.file_path = m2.file_path
WHERE m1.commit_hash NOT IN (
    SELECT commit_hash FROM modifies
    GROUP BY commit_hash HAVING COUNT(*) > ?1
)
GROUP BY m1.file_path, m2.file_path
HAVING COUNT(DISTINCT m1.commit_hash) >= ?2;
```

## Tree-sitter (gotreesitter)

### Language Support Priority

| Tier | Languages | Reason |
|------|-----------|--------|
| 1 (v1) | Go, TypeScript, Python | Most common in coding agent ecosystem |
| 2 (v1.1) | Rust, Java | High demand, well-supported grammars |
| 3 (future) | C/C++, Ruby, PHP, C#, Swift, Kotlin | On-demand |

### Go Files: Prefer go/ast

For Go source files, use `go/ast` + `go/parser` instead of tree-sitter.
This provides higher precision for symbol extraction (full type resolution,
interface satisfaction, package-qualified names) with zero added dependency
since these are stdlib packages.

### Symbol Extraction

- **Composite ID**: `"{file_path}:{kind}:{name}:{start_line}"` handles overloaded
  names and ensures uniqueness.
- **Method receivers**: For Go methods, include receiver type in the symbol ID:
  `"pkg/foo.go:method:Bar.Baz:42"`.
- **Confidence scoring for CALLS edges**:
  - 1.0: Exact static call, unambiguous name resolution
  - 0.8: Receiver/method dispatch (type could be interface)
  - 0.5: Same-name match across packages (fuzzy)

### Import Resolution

| Language | Import syntax | Resolution strategy |
|----------|--------------|---------------------|
| Go | `"github.com/pkg/..."` | Module path -> local directory |
| TypeScript | `'./utils'`, `'../lib/format'` | Relative path + extension resolution |
| Python | `from pkg import module` | Dotted path -> directory/file |
| Rust | `use crate::module` | Crate-relative path |
| Java | `import com.pkg.Class` | Package path -> directory structure |

### Query Files

Store tree-sitter S-expression queries as embedded `.scm` files:
```
infrastructure/treesitter/queries/
  go.scm
  typescript.scm
  python.scm
  rust.scm
  java.scm
```

Embed via `//go:embed queries/*.scm` for zero-overhead at runtime.

### Language Detection

File extension mapping with fallback:
```
.go -> Go           .ts/.tsx -> TypeScript    .js/.jsx -> JavaScript (use TS parser)
.py -> Python        .rs -> Rust               .java -> Java
```

Skip files with no recognized extension or in excluded directories.

### Excluded Paths

Reuse the existing `skipDirs` pattern from `infrastructure/git/client.go`:
```
node_modules, vendor, dist, build, target, __pycache__, .next, out, coverage
```

Also skip:
- Binary files (detected by null bytes in first 512 bytes)
- Generated files (containing `DO NOT EDIT` / `@generated` in first 10 lines)
- Lock files (reuse `pkg/filter/patterns.go` lock file patterns)

## Build and CI

### Simplified Build (No CGo)

Since `modernc.org/sqlite` is pure Go, no CGo toolchain is required:

```makefile
build:
	go build -o git-agent .

test:
	go test -count=1 ./...
```

- No separate `build-graph` or `test-graph` targets needed
- Single CI job per platform (no graph/non-graph matrix)
- Cross-compilation works out of the box (`GOOS=linux GOARCH=amd64 go build`)
- No shared library bundling or `CGO_ENABLED=1` requirement

### Binary Size Budget

| Component | Estimated size |
|-----------|---------------|
| Current binary | ~7.2 MB |
| modernc.org/sqlite | +8-12 MB |
| gotreesitter (5 grammars) | +5-10 MB |
| Total | ~20-30 MB |
| Target ceiling | 35 MB |

## Action Capture and Timeline (P1b)

### Capture Performance

The `capture` command is called by agent hooks after every tool call. It must
complete in under 200ms to avoid degrading agent UX.

- **Delta-based tracking**: use `capture_baseline` table to track file content
  hashes (`git hash-object`). Only attribute files whose hash changed since the
  last capture. This prevents diff accumulation where later captures would
  incorrectly include changes from prior tool calls.
- **Minimal work**: compute hashes, diff delta files, write 2-3 rows + edges, update baseline, exit
- **No schema recomputation**: do not recompute CO_CHANGED during capture
- **No LLM calls**: action summaries are filled later by `timeline --compress`
- **Lock timeout**: if the write lock cannot be acquired in 100ms, skip silently
- **Batch optimization**: if multiple files changed, create all action_modifies
  rows and capture_baseline updates in a single transaction
- **Hash cost**: `git hash-object` adds ~1ms per file; negligible for typical
  agent edits (1-5 files per tool call)
- **Deleted files**: if a file appears in `git diff` but not on disk, use
  sentinel hash `"deleted"` instead of `git hash-object` (which would fail)
- **Baseline cleanup**: purge entries older than 24h that are not in the current
  `git diff` to prevent unbounded growth

### Session Lifecycle

| Event | Behavior |
|-------|----------|
| First capture for a source + instance_id | Create new session row |
| Subsequent capture within 30 min (same source + instance) | Append to existing session |
| No capture for 30 min | Next capture starts new session, auto-closes old one |
| `--end-session` flag | Explicitly close the session |
| `graph reset` | Deletes all sessions and actions |
| Two concurrent agents of same source | Separate sessions via different `instance_id` (defaults to `$PPID`) |

The 30-minute timeout is configurable in `.git-agent/config.yml`:
```yaml
graph:
  session_timeout_minutes: 30
```

### Diff Storage

- Store diffs in a SQLite `TEXT` column (not external files)
- Truncate at 100KB with `[truncated]` marker
- Expected median: 1-5KB per action (typical agent edits are small)
- For a 200-action session, expect ~200KB-1MB of diff data
- Purge old action data: `graph reset --actions-before 2026-03-01`

### Timeline Compression

When `--compress` is used, the LLM receives grouped diffs per session:

```
Session s1 (claude-code, 2026-04-06 14:00-14:15, 3 actions):
- Edit src/main.go: [diff]
- Write src/main_test.go: [diff]
- Bash: go test ./... (exit 0)

Summarize this session in one sentence.
```

Guidelines:
- Group actions by session before sending to LLM
- Truncate total context to fit model limits (keep most recent actions if overflow)
- Cache summaries: once computed, write to session summary / action summary columns
- Subsequent `timeline --compress` calls return cached summaries without re-calling LLM

### Hook Configuration

**Claude Code** is the primary integration target. The hook:
- Must exit 0 in all cases (never block the agent)
- Should complete in <200ms
- Should not produce stdout output (agents may parse it)
- Can write warnings to stderr (agent ignores stderr)

Recommended setup command (future):
```bash
git-agent graph setup-hooks --agent claude-code
# Writes PostToolUse hook to ~/.claude/settings.json
```

**Universal fallback** for agents without hooks:
- Git `post-commit` hook: `git-agent graph capture --source git-hook`
- Captures at commit granularity instead of action granularity
- Better than nothing; loses per-tool-call attribution

## Security

- **Parameterized queries**: All queries use `?` placeholders with parameter
  binding. Never concatenate user input into SQL strings.
- **File permissions**: `.git-agent/graph.db` uses 0644. Query commands open
  read-only (`PRAGMA query_only=ON`).
- **No secrets in graph**: Graph contains only structural data (paths, hashes,
  author names/emails from git log). Action diffs may contain code snippets but
  never credentials (diffs come from `git diff`, which respects `.gitignore`).
- **Gitignore**: `graph.db`, `graph.db-wal`, and `graph.db-shm` are
  auto-added to `.gitignore` during `graph index`.

## Error Handling Patterns

| Scenario | Behavior |
|----------|----------|
| No git repository | Exit 1, `{"error": "not a git repository"}` |
| No graph DB exists | Exit 3, `{"error": "graph not indexed", "hint": "run 'git-agent graph index'"}` |
| SQLite busy/locked | Retry via busy_timeout (5s); if exhausted, exit 1 |
| Database disk image is malformed | Exit 1, suggest `git-agent graph reset` |
| Lock contention (capture) | Skip silently, exit 0, warn on stderr |
| Force-push / history rewrite | Auto-detect via `merge-base --is-ancestor`, fall back to full re-index |
| Schema version mismatch (minor) | Auto-migrate forward (ALTER TABLE, CREATE TABLE IF NOT EXISTS) |
| Schema version mismatch (major) | Exit 1, suggest `graph reset` with warning about action data loss |
| Unsupported language | Skip file, warn in verbose mode |
| Tree-sitter parse error | Skip file, warn in verbose mode |
| LLM not configured (`--compress`/`diagnose`) | Exit 1, `{"error": "LLM endpoint not configured"}` |
| LLM call fails | Exit 1, `{"error": "LLM request failed", "detail": "..."}` |
