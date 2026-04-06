# Architecture: git-agent graph

## Clean Architecture Placement

The graph feature follows the existing 4-layer inward-dependency pattern:

```
cmd/graph*.go  -->  application/graph_*.go  -->  domain/graph/  <--  infrastructure/graph/
                                                                <--  infrastructure/treesitter/
```

Domain has zero external imports. KuzuDB and Tree-sitter live exclusively in
`infrastructure/`.

## Package Structure

```
git-agent-cli/
  domain/
    graph/
      repository.go          # GraphRepository interface
      index.go               # IndexRequest, IndexResult, IndexStatus DTOs
      query.go               # BlastRadiusRequest, BlastRadiusResult, etc.
      session.go             # SessionNode, ActionNode, CaptureRequest, CaptureResult DTOs
      timeline.go            # TimelineRequest, TimelineResult, DiagnoseRequest, DiagnoseResult DTOs
      nodes.go               # CommitNode, FileNode, SymbolNode, AuthorNode
      edges.go               # Edge types, CO_CHANGED weight

  application/
    graph_service.go          # GraphService: Index, BlastRadius, Hotspots, Ownership, Status, Reset
    graph_capture_service.go  # CaptureService: Capture, EndSession, Timeline
    graph_diagnose_service.go # DiagnoseService: Diagnose (P2, requires LLM)

  infrastructure/
    graph/
      kuzu_client.go          # KuzuDB connection, schema DDL, lifecycle
      kuzu_repository.go      # GraphRepository impl: MERGE, COPY FROM, Cypher queries
      indexer.go              # Git history walker + incremental logic
      co_change.go            # CO_CHANGED edge computation

    treesitter/
      parser.go               # Language detection + gotreesitter wrapper
      extractor.go            # Symbol extraction (functions, classes, calls, imports)
      queries/                # Embedded .scm query files per language
        go.scm
        typescript.scm
        python.scm
        rust.scm
        java.scm

  cmd/
    graph.go                  # Root "graph" command (Cobra)
    graph_index.go            # "graph index" subcommand
    graph_blast_radius.go     # "graph blast-radius" subcommand
    graph_capture.go          # "graph capture" subcommand (P1)
    graph_timeline.go         # "graph timeline" subcommand (P1)
    graph_hotspots.go         # "graph hotspots" subcommand (P1)
    graph_ownership.go        # "graph ownership" subcommand (P1)
    graph_diagnose.go         # "graph diagnose" subcommand (P2)
    graph_status.go           # "graph status" subcommand
    graph_reset.go            # "graph reset" subcommand

  pkg/
    graph/
      format.go               # JSON/text output formatting for graph results
```

## Domain Interfaces

### GraphRepository

```go
type GraphRepository interface {
    // Lifecycle
    Open(ctx context.Context) error
    Close() error
    InitSchema(ctx context.Context) error
    Drop(ctx context.Context) error

    // Write (indexing)
    UpsertCommit(ctx context.Context, c CommitNode) error
    UpsertAuthor(ctx context.Context, a AuthorNode) error
    UpsertFile(ctx context.Context, f FileNode) error
    CreateModifies(ctx context.Context, commitHash, filePath, status string, additions, deletions int) error
    CreateAuthored(ctx context.Context, authorEmail, commitHash string) error
    ReplaceFileSymbols(ctx context.Context, filePath string, symbols []SymbolNode, calls []CallEdge, imports []ImportEdge) error
    RecomputeCoChanged(ctx context.Context, minCount int) error

    // State
    GetLastIndexedCommit(ctx context.Context) (string, error)
    SetLastIndexedCommit(ctx context.Context, hash string) error
    GetStats(ctx context.Context) (*GraphStats, error)

    // Read (queries)
    BlastRadius(ctx context.Context, req BlastRadiusRequest) (*BlastRadiusResult, error)
    Hotspots(ctx context.Context, req HotspotsRequest) (*HotspotsResult, error)
    Ownership(ctx context.Context, req OwnershipRequest) (*OwnershipResult, error)

    // Session/Action tracking
    UpsertSession(ctx context.Context, s SessionNode) error
    CreateAction(ctx context.Context, a ActionNode) error
    CreateSessionContains(ctx context.Context, sessionID, actionID string) error
    CreateActionModifies(ctx context.Context, actionID, filePath string, additions, deletions int) error
    CreateActionProduces(ctx context.Context, actionID, commitHash string) error
    GetActiveSession(ctx context.Context, source string, timeoutMinutes int) (*SessionNode, error)
    EndSession(ctx context.Context, sessionID string) error
    Timeline(ctx context.Context, req TimelineRequest) (*TimelineResult, error)
    ActionsForFiles(ctx context.Context, filePaths []string, since int64) ([]ActionNode, error)

    // Raw Cypher (power user)
    Query(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}
```

### ASTParser

```go
type ASTParser interface {
    Parse(ctx context.Context, language string, source []byte) (*ParseResult, error)
    SupportedLanguages() []string
}

type ParseResult struct {
    Symbols []SymbolNode
    Calls   []CallEdge    // {from: symbol ID, to: symbol name, confidence}
    Imports []ImportEdge  // {from: file path, import_path: raw string, resolved: file path}
}
```

## Application Services

### GraphService (index + queries)

```go
type GraphService struct {
    repo   graph.GraphRepository
    parser graph.ASTParser
    git    GraphGitClient
}

// GraphGitClient extends the existing git client interface
type GraphGitClient interface {
    CommitLogDetailed(ctx context.Context, since string, max int) ([]graph.CommitInfo, error)
    FileContentAt(ctx context.Context, commitHash, filePath string) ([]byte, error)
    CurrentHead(ctx context.Context) (string, error)
    Diff(ctx context.Context) (string, error)            // unstaged + staged diff
    DiffFiles(ctx context.Context) ([]string, error)     // list of changed file paths
}
```

| Method | Description |
|--------|-------------|
| `Index(ctx, req IndexRequest) (*IndexResult, error)` | Full or incremental index |
| `BlastRadius(ctx, req BlastRadiusRequest) (*BlastRadiusResult, error)` | Co-change + call chain |
| `Hotspots(ctx, req HotspotsRequest) (*HotspotsResult, error)` | Ranked change frequency |
| `Ownership(ctx, req OwnershipRequest) (*OwnershipResult, error)` | Author contribution |
| `Status(ctx) (*GraphStatus, error)` | DB metadata + node/edge counts |
| `Reset(ctx) error` | Drop database directory |

### CaptureService (session/action tracking)

```go
type CaptureService struct {
    repo graph.GraphRepository
    git  GraphGitClient
}
```

| Method | Description |
|--------|-------------|
| `Capture(ctx, req CaptureRequest) (*CaptureResult, error)` | Record one action: read diff, create Session/Action nodes + edges |
| `EndSession(ctx, sessionID string) error` | Mark session as ended |
| `Timeline(ctx, req TimelineRequest) (*TimelineResult, error)` | Query sessions/actions with filters |

### DiagnoseService (P2, requires LLM)

```go
type DiagnoseService struct {
    repo    graph.GraphRepository
    graph   *GraphService       // reuses BlastRadius
    capture *CaptureService     // reuses Timeline/ActionsForFiles
    llm     LLMClient           // existing OpenAI-compatible client
}

// LLMClient reuses the existing infrastructure/openai client interface
type LLMClient interface {
    Complete(ctx context.Context, prompt string) (string, error)
}
```

| Method | Description |
|--------|-------------|
| `Diagnose(ctx, req DiagnoseRequest) (*DiagnoseResult, error)` | Combine blast-radius + timeline + LLM reasoning to identify introducing action |

## CLI Wiring (Cobra)

Following the existing pattern in `cmd/commit.go` and `cmd/init.go`:

```go
// cmd/graph.go
var graphCmd = &cobra.Command{
    Use:   "graph",
    Short: "Query the code knowledge graph",
}

func init() {
    graphCmd.AddCommand(graphIndexCmd)
    graphCmd.AddCommand(graphBlastRadiusCmd)
    graphCmd.AddCommand(graphStatusCmd)
    graphCmd.AddCommand(graphResetCmd)
    rootCmd.AddCommand(graphCmd)
}
```

Each subcommand constructs the service with wired dependencies:

```go
// cmd/graph_index.go
var graphIndexCmd = &cobra.Command{
    Use:   "index",
    Short: "Build or update the code graph",
    RunE: func(cmd *cobra.Command, args []string) error {
        repo := kuzu.NewRepository(graphDBPath())
        parser := treesitter.NewParser()
        gitClient := git.NewClient()
        svc := application.NewGraphService(repo, parser, gitClient)
        defer repo.Close()

        result, err := svc.Index(cmd.Context(), application.IndexRequest{
            Force:             force,
            MaxCommits:        maxCommits,
            AST:               ast,
            MaxFilesPerCommit: maxFilesPerCommit,
        })
        // ... format and output result
    },
}
```

## Index Algorithm

```
1. Open KuzuDB at .git-agent/graph.db (create if missing)
2. InitSchema (CREATE NODE/REL TABLE IF NOT EXISTS)
3. Read IndexState.last_indexed_commit
4. git log lastHash..HEAD --format=... --name-status
5. For each commit (in chronological order):
   a. MERGE Commit node
   b. MERGE Author node + AUTHORED edge
   c. For each modified file:
      - MERGE File node
      - CREATE MODIFIES edge
      - If --ast and file extension is supported:
        * git show commitHash:filePath
        * Tree-sitter parse -> extract symbols, calls, imports
        * ReplaceFileSymbols (DELETE old + CREATE new)
6. RecomputeCoChanged (MERGE CO_CHANGED edges)
7. SetLastIndexedCommit(HEAD)
8. Return IndexResult with stats
```

For initial full index (no lastHash), use COPY FROM with temporary CSV files
for bulk loading. This is 53x faster than row-by-row MERGE.

## Blast Radius Query

Two-phase query:

**Phase 1: Co-change neighbors**
```cypher
MATCH (target:File {path: $path})-[cc:CO_CHANGED]-(neighbor:File)
WHERE cc.coupling_count >= $minCount
RETURN neighbor.path, cc.coupling_count, cc.coupling_strength, 'co-change' AS reason
ORDER BY cc.coupling_strength DESC
LIMIT $top
```

**Phase 2: Import/call chain** (when AST is indexed)
```cypher
MATCH (target:File {path: $path})-[:CONTAINS]->(sym:Symbol)
      -[:CALLS*1..$depth]->(callee:Symbol)<-[:CONTAINS]-(caller_file:File)
WHERE caller_file.path <> target.path
RETURN DISTINCT caller_file.path, 'call-dependency' AS reason
```

```cypher
MATCH (importer:File)-[:IMPORTS]->(target:File {path: $path})
RETURN importer.path, 'imports' AS reason
```

**Phase 3: Symbol-level entry point** (when `--symbol` is provided instead of file path)
```cypher
MATCH (target:Symbol)
WHERE target.name = $symbolName AND target.file_path = $filePath
MATCH (caller:Symbol)-[:CALLS*1..$depth]->(target)
RETURN DISTINCT
    caller.file_path AS file,
    caller.name AS symbol,
    caller.kind AS kind
ORDER BY file
```

Results are merged, deduplicated, and ranked.

## Build Tag Isolation

```go
//go:build graph

package kuzu

import kuzu "github.com/kuzudb/go-kuzu"
// ...
```

- `make build` -- pure Go, no CGo, no graph support
- `make build-graph` -- `go build -tags graph`, includes KuzuDB
- `make test` -- tests pass without graph tag (graph tests skip)
- `make test-graph` -- `go test -tags graph ./...`

The `cmd/graph.go` file conditionally registers the command:

```go
//go:build graph

func init() {
    rootCmd.AddCommand(graphCmd)
}
```

Without the build tag, `git-agent graph` is not available.

## Capture Algorithm

```
1. Read git diff (unstaged + staged)
2. If diff is empty, return {"skipped": true} and exit 0
3. Open KuzuDB at .git-agent/graph.db (create if missing, init schema)
4. Find active session for this source:
   MATCH (s:Session {source: $source})
   WHERE s.ended_at IS NULL AND s.started_at > (now - timeout)
   RETURN s
5. If no active session:
   a. Create Session node (id=UUID, source, started_at=now)
6. Create Action node:
   a. id = "{session_id}:{next_sequence}"
   b. tool = --tool flag value
   c. diff = git diff output (truncated at 100KB)
   d. files_changed = parsed from diff headers
   e. timestamp = now
   f. message = --message flag value
7. Create SESSION_CONTAINS edge
8. For each changed file:
   a. MERGE File node (ensure exists)
   b. Create ACTION_MODIFIES edge with additions/deletions
9. If --end-session: set Session.ended_at = now
10. Return CaptureResult
```

Performance target: <200ms total. No LLM calls. No CO_CHANGED recomputation.

## Diagnose Algorithm (P2)

```
1. Parse input: bug description or file path
2. If file path:
   a. BlastRadius(filePath) -> affected files
   b. ActionsForFiles(affected + target, --since) -> candidate actions
3. If description:
   a. LLM: extract likely file paths from description
   b. BlastRadius for each -> affected files
   c. ActionsForFiles(affected, --since) -> candidate actions
4. Build context for LLM:
   a. Bug description
   b. Candidate actions (action ID, tool, timestamp, diff excerpt, files)
   c. Blast radius summary
5. LLM prompt: "Given this bug and these recent actions, which action
   most likely introduced it? Explain why and suggest a fix."
6. Parse LLM response into DiagnoseResult
7. Return ranked suspects with explanations
```

## Hook Integration Architecture

```
Agent (Claude Code)
  |
  | PostToolUse hook fires after Edit/Write/Bash
  |
  v
git-agent graph capture --source claude-code --tool $CLAUDE_TOOL_NAME
  |
  | 1. git diff (fast, local)
  | 2. Write Session/Action nodes to KuzuDB
  | 3. Exit 0 (never block agent)
  |
  v
.git-agent/graph.db (Session, Action, ACTION_MODIFIES edges)
  |
  | Later, user or agent queries:
  |
  +---> git-agent graph timeline --since 1h
  +---> git-agent graph diagnose "test failures"
  +---> git-agent graph blast-radius src/foo.go  (enhanced with action data)
```

Key design constraint: `capture` must never fail the hook. If KuzuDB is
locked or corrupt, log to stderr and exit 0. The agent must not be blocked.

## Action-to-Commit Linking

When `git-agent commit` runs and produces a commit, it should link
preceding uncommitted actions to the new Commit node:

```
1. After successful git commit, get the new commit hash
2. Query: all Actions where ACTION_PRODUCES is empty
   AND action.timestamp > last_commit_timestamp
   AND action files overlap with committed files
3. Create ACTION_PRODUCES edge for each matching action
```

This runs inside the existing commit flow (in `cmd/commit.go`), not as a
separate command. It bridges action-level and commit-level history.

## Concurrency and Locking

- **Index**: Single writer. Use file lock (`.git-agent/graph.lock`) to prevent
  concurrent indexing from multiple terminals.
- **Capture**: Lightweight writer. Uses the same lock but with a short timeout
  (100ms). If lock is held, skip silently (agent must not be blocked).
- **Query**: KuzuDB supports concurrent reads. Open in read-only mode.
- **Reset**: Acquire write lock, then delete directory.

## Error Handling

- Graph not indexed: exit code 3 with `{"error": "graph not indexed", "hint": "run 'git-agent graph index'"}`
- KuzuDB corruption: `graph reset` provides manual recovery
- Lock contention: `{"error": "graph is being indexed by another process"}`
- Capture lock contention: skip silently, exit 0 (never block agent hooks)
- Unsupported language (AST): Skip file, log warning in verbose mode
- LLM unavailable (for `--compress` / `diagnose`): exit 1 with `{"error": "LLM endpoint not configured", "hint": "set endpoint in config"}`

## Mermaid: Component Diagram

```mermaid
graph LR
    subgraph cmd
        G[graph.go]
        GI[graph_index.go]
        GBR[graph_blast_radius.go]
        GC[graph_capture.go]
        GTL[graph_timeline.go]
        GDX[graph_diagnose.go]
        GS[graph_status.go]
        GR[graph_reset.go]
    end

    subgraph application
        SVC[GraphService]
        CAP[CaptureService]
        DGN[DiagnoseService]
    end

    subgraph domain/graph
        REPO[GraphRepository]
        AST[ASTParser]
        DTO[DTOs]
    end

    subgraph infrastructure
        KUZU[kuzu/repository.go]
        TS[treesitter/parser.go]
        GIT[git/client.go]
        LLM[openai/client.go]
    end

    subgraph external
        DB[KuzuDB .git-agent/graph.db]
        GITCLI[git CLI]
    end

    G --> GI & GBR & GC & GTL & GDX & GS & GR
    GI & GBR & GS & GR --> SVC
    GC & GTL --> CAP
    GDX --> DGN
    DGN --> SVC & CAP & LLM
    SVC --> REPO & AST
    CAP --> REPO
    KUZU -.implements.-> REPO
    TS -.implements.-> AST
    SVC --> GIT
    CAP --> GIT
    KUZU --> DB
    GIT --> GITCLI
    TS --> |gotreesitter| TS
```
