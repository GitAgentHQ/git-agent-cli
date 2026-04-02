# Best Practices: git-agent graph

## KuzuDB

### Schema Design

- **Natural keys over SERIAL**: Use commit hash, file path, composite symbol ID
  as primary keys. Enables idempotent MERGE operations -- re-indexing the same
  commit is a no-op.
- **Pre-declare all property types**: KuzuDB requires explicit DDL. All node
  and relationship tables must be created before data insertion.
- **`IF NOT EXISTS`**: All `CREATE TABLE` statements use `IF NOT EXISTS` for
  safe schema initialization on every startup.

### Performance Tuning

| Setting | Value | Rationale |
|---------|-------|-----------|
| BufferPoolSize | 256 MB | CLI tool, not a server. Default 80% RAM is wasteful. |
| MaxNumThreads | 4 | Don't saturate user's machine. |
| EnableCompression | true | Reduces disk I/O, smaller DB files. |
| ReadOnly | true (for query commands) | Allows concurrent reads without lock contention. |

### Bulk Loading

- **Initial full index**: Write nodes/edges to temporary CSV files, then
  `COPY FROM` into KuzuDB. This is 53x faster than row-by-row INSERT
  (benchmarked: 100K nodes + 2.4M edges in 0.58s via COPY vs 30.64s via INSERT).
- **Incremental updates**: Use `MERGE` with `ON CREATE`/`ON MATCH` for
  subsequent commits after the initial bulk load.

### Incremental Strategy

| Data type | Strategy | Reason |
|-----------|----------|--------|
| Commits, Authors | MERGE | Append-only; MERGE is idempotent |
| MODIFIES, AUTHORED | MERGE | Edge follows commit lifecycle |
| Symbols | DELETE+CREATE per file | AST must be fully reparsed; diffing individual symbols is fragile |
| CONTAINS, CALLS | DELETE+CREATE per file | Follow symbol lifecycle |
| IMPORTS | DELETE+CREATE per file | Follow file parse lifecycle |
| CO_CHANGED | MERGE with ON MATCH | Recompute coupling counts after all new commits indexed |

### Query Optimization

- Bound variable-length paths: always `*1..N` with explicit max depth (e.g., 3).
  Unbounded traversals explode on dense graphs.
- Use `shortestPath()` when only minimum distance matters.
- Use `EXPLAIN` / `PROFILE` during development to inspect execution plans.
- KuzuDB zone maps on numeric columns enable skip-scan for `timestamp`,
  `start_line`, `coupling_count` filters.
- Parameterized queries: always use `$param` syntax, never string concatenation
  (prevents injection, enables plan caching).

### CO_CHANGED Computation

Formula (CodeScene methodology):
```
coupling_strength = coupling_count / max(commits_file_a, commits_file_b)
```

Thresholds:
- Minimum co-change count: 3 (filters noise from bulk reformats)
- Maximum files per commit: 50 (skip merge commits and bulk operations)
- Both thresholds are configurable via `--min-count` and `--max-files-per-commit`

## Tree-sitter (gotreesitter)

### Language Support Priority

| Tier | Languages | Reason |
|------|-----------|--------|
| 1 (v1) | Go, TypeScript, Python | Most common in coding agent ecosystem |
| 2 (v1.1) | Rust, Java | High demand, well-supported grammars |
| 3 (future) | C/C++, Ruby, PHP, C#, Swift, Kotlin | On-demand |

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

### Build Tag Strategy

```makefile
# Default build: pure Go, no CGo, no graph
build:
	go build -o git-agent .

# Graph-enabled build: CGo required
build-graph:
	CGO_ENABLED=1 go build -tags graph -o git-agent .

# Tests: default excludes graph
test:
	go test -count=1 ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...

# Graph tests: requires CGo + KuzuDB
test-graph:
	CGO_ENABLED=1 go test -tags graph -count=1 ./...
```

### CI Matrix

| Job | Build tags | CGO | Platforms |
|-----|-----------|-----|-----------|
| Default | (none) | 0 | linux-amd64, darwin-arm64, darwin-amd64 |
| Graph | `graph` | 1 | linux-amd64, darwin-arm64 |

### Binary Size Budget

| Component | Estimated size |
|-----------|---------------|
| Current binary | ~7.2 MB |
| go-kuzu (KuzuDB shared lib) | +20-30 MB |
| gotreesitter (5 grammars) | +5-10 MB |
| Total graph-enabled | ~35-45 MB |
| Target ceiling | 50 MB |

## Security

- **Parameterized Cypher**: All queries use `$param` placeholders. Never
  concatenate user input into Cypher strings.
- **File permissions**: `.git-agent/graph.db/` uses 0755 (directory) and
  0644 (files). Query commands open read-only.
- **No secrets in graph**: Graph contains only structural data (paths, hashes,
  author names/emails from git log). No file contents stored.
- **Gitignore**: `graph.db/` is auto-added to `.gitignore` during `graph index`.

## Error Handling Patterns

| Scenario | Behavior |
|----------|----------|
| No git repository | Exit 1, `{"error": "not a git repository"}` |
| No graph DB exists | Exit 3, `{"error": "graph not indexed", "hint": "run 'git-agent graph index'"}` |
| DB corruption | Exit 1, suggest `git-agent graph reset` |
| Lock contention | Exit 1, `{"error": "graph is being indexed by another process"}` |
| Unsupported language | Skip file, warn in verbose mode |
| Tree-sitter parse error | Skip file, warn in verbose mode |
| KuzuDB query timeout | Exit 1, `{"error": "query timed out"}` |
