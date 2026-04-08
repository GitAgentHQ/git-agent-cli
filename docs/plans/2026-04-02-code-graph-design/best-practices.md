# Best Practices: git-agent graph

## SQLite (modernc.org/sqlite)

### Schema Design

- **Relational tables model the property graph**: Node tables (`commits`,
  `files`, `authors`) and join tables for edges (`authored`, `modifies`,
  `co_changed`). 13 tables total.
- **Natural keys over surrogates**: Use commit hash, file path as primary
  keys. Enables `INSERT OR IGNORE` / `INSERT OR REPLACE` for idempotent
  operations -- re-indexing the same commit is a no-op.
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
| CO_CHANGED | Incremental: delete pairs involving touched files + recompute those pairs; full recompute on force re-index or >500 new commits | Avoids O(n^2) self-join on entire modifies table for small incremental updates |

### Query Patterns

- **Co-change pairs**: Simple `JOIN` on the `modifies` table, grouping by
  commit to find files that changed together.
- **Impact query (co-change)**: Direct `SELECT` on `co_changed` filtered by
  `coupling_strength` threshold.

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

## Excluded Paths

During indexing, skip files that add noise without value:

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
| Total | ~15-20 MB |

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
| Manual DB delete (`rm .git-agent/graph.db*`) | Deletes all sessions and actions |
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

### Timeline Compression (P2)

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

**Universal fallback** for agents without hooks:
- Git `post-commit` hook: `git-agent capture --source git-hook`
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
  auto-added to `.gitignore` during EnsureIndex.

## Error Handling Patterns

| Scenario | Behavior |
|----------|----------|
| No git repository | Exit 1, `{"error": "not a git repository"}` |
| EnsureIndex fails | Exit 3, `{"error": "auto-index failed", "detail": "..."}` |
| SQLite busy/locked | Retry via busy_timeout (5s); if exhausted, exit 1 |
| Database disk image is malformed | Exit 1, suggest `rm .git-agent/graph.db*` |
| Lock contention (capture) | Skip silently, exit 0, warn on stderr |
| Force-push / history rewrite | Auto-detect via `merge-base --is-ancestor`, fall back to full re-index |
| Schema version mismatch (minor) | Auto-migrate forward (ALTER TABLE, CREATE TABLE IF NOT EXISTS) |
| Schema version mismatch (major) | Exit 1, suggest deleting graph.db (warns about action data loss) |
| LLM not configured (`--compress`) | Exit 1, `{"error": "LLM endpoint not configured"}` |
| LLM call fails | Exit 1, `{"error": "LLM request failed", "detail": "..."}` |
