# Task 001: Event Log domain types and append-only schema foundation

**type**: setup
**depends-on**: []

## Files
- create: `domain/graph/event.go` — `EventRecord`, `EventKind`/`EventSource` string types + consts, `EventHasher` port, audit-surface types (`VerifyResult`, `ChainBreak`, `ChainBreakKind`)
- modify: `infrastructure/graph/sqlite_client.go` — add `events` + `event_files` DDL to `schemaStatements` (`:332`); bump `CurrentSchemaVersion` (`:16`); remove `capture_baseline` DDL (`:426-430`)
- modify: `domain/graph/session.go` — remove `CaptureBaseline` value object (`:51-56`); note `ActionNode`/`SessionNode` are now Projection DTOs
- modify: `domain/graph/repository.go` — add `AppendEvent`/`HeadHash`/`StreamEvents`/`VerifyChain` signatures (`:5`); remove `GetCaptureBaseline`/`UpdateCaptureBaseline`/`CleanupCaptureBaseline` (`:54-58`)
- modify: `infrastructure/graph/sqlite_repository.go` — add compile-stub impls of the 4 new methods returning a not-implemented error; remove the three baseline method impls (`:996-1067`); remove the baseline half of `CreateActionBatch` (`:749-765`)

## BDD Scenario(s)
No BDD scenario — foundation task.

## What to implement

Pure contracts only — no bodies (stubs return a sentinel error so the build links).

### `domain/graph/event.go`

Define string kinds/sources with constants including `"unknown"` and `"out-of-band"`
(field list copied verbatim from architecture.md "Clean-Architecture Placement"):

```go
type EventKind string

const (
    EventKindTool      EventKind = "tool"
    EventKindOutcome   EventKind = "outcome"
    EventKindOutOfBand EventKind = "out-of-band"
)

type EventSource string

const (
    EventSourceClaudeCode EventSource = "claude-code"
    EventSourceCursor     EventSource = "cursor"
    EventSourceHuman      EventSource = "human"
    EventSourceUnknown    EventSource = "unknown"
)

// EventRecord is the immutable, hash-chained source-of-truth record.
type EventRecord struct {
    Seq            int64
    EventID        string
    RecordedAt     int64
    Source         EventSource
    InstanceID     string
    Kind           EventKind
    HookEventName  string
    ToolName       string
    Cwd            string
    TranscriptPath string
    PermissionMode string
    PayloadRaw     []byte
    PayloadSize    int64
    Truncated      bool
    // Outcome fields (zero unless Kind == EventKindOutcome)
    Command        string
    ExitCode       *int
    ExitCodeSource string // "reported" | "inferred"
    IsTest         bool
    IsBuild        bool
    TestName       string
    // Chain
    PrevHash       string
    ThisHash       string
}

// EventHasher canonicalizes-and-hashes an EventRecord. Hashing (SHA-256) is an
// infrastructure concern, so the domain depends on this port (peer of
// SessionIDGenerator).
type EventHasher interface {
    Hash(prevHash string, e EventRecord) string
}
```

Audit-surface types per architecture.md "Audit Surface":

```go
type ChainBreakKind string

const (
    ChainBreakRowEdited    ChainBreakKind = "ROW_EDITED"
    ChainBreakRowDeleted   ChainBreakKind = "ROW_DELETED"
    ChainBreakRowInserted  ChainBreakKind = "ROW_INSERTED"
    ChainBreakRowReordered ChainBreakKind = "ROW_REORDERED"
)

type ChainBreak struct {
    Seq              int64
    EventID          string
    Kind             ChainBreakKind
    ExpectedThisHash string
    StoredThisHash   string
}

type VerifyResult struct {
    Status         string // "ok" | "broken"
    EventsTotal    int64
    EventsVerified int64
    FirstBreak     *ChainBreak
}
```

### `domain/graph/repository.go`

Add to the `GraphRepository` interface (signatures per architecture.md "Hash Chain"):

```go
// Event Log (append-only chain)
AppendEvent(ctx context.Context, e EventRecord) (EventRecord, error) // only writer into events; assigns seq + this_hash
HeadHash(ctx context.Context) (string, error)                        // current chain head (prev_hash for next)
StreamEvents(ctx context.Context, sinceSeq int64) (EventCursor, error)
VerifyChain(ctx context.Context) (VerifyResult, error)
```

Define the `EventCursor` iterator contract (in `event.go` or `repository.go`) — e.g.
`Next() bool`, `Event() EventRecord`, `Err() error`, `Close() error`. No body.

Remove the three `GetCaptureBaseline`/`UpdateCaptureBaseline`/`CleanupCaptureBaseline`
signatures.

### `infrastructure/graph/sqlite_client.go`

Append the `events` and `event_files` `CREATE TABLE`/`CREATE INDEX` statements
(copied verbatim from architecture.md "Schema") to `schemaStatements`. Remove the
`capture_baseline` `CREATE TABLE`. Bump `CurrentSchemaVersion` from `1` to `2`.

### `infrastructure/graph/sqlite_repository.go`

Add stub method bodies for `AppendEvent`/`HeadHash`/`StreamEvents`/`VerifyChain`
that return a not-implemented sentinel error (`errors.New("not implemented")` or
existing project pattern) so the package compiles. Delete the three baseline impls.
In `CreateActionBatch`, delete the `if len(baselineUpdates) > 0 { ... }` block
(`:749-765`); keep the `baselineUpdates` parameter for now (call sites are rewired
in later tasks) or drop it consistently — choose whichever keeps the build green.

### `domain/graph/session.go`

Delete the `CaptureBaseline` struct. Add a short doc comment on `SessionNode` and
`ActionNode` noting they are Projection DTOs derived from the Event Log, not
source-of-truth.

## Steps
1. Write `domain/graph/event.go` with the types, consts, port, and audit types above.
2. Edit `domain/graph/repository.go`: add the 4 method signatures + `EventCursor`; remove the 3 baseline signatures.
3. Edit `domain/graph/session.go`: remove `CaptureBaseline`; annotate `SessionNode`/`ActionNode` as DTOs.
4. Edit `infrastructure/graph/sqlite_client.go`: add DDL, bump version, drop `capture_baseline`.
5. Edit `infrastructure/graph/sqlite_repository.go`: add 4 stub impls; remove baseline impls; remove baseline branch in `CreateActionBatch`.
6. Resolve any now-dead references to baseline/`CaptureBaseline` in callers so the build links.

## Verification
- `go build ./...` — compiles (stubs link; no unresolved references to removed baseline symbols).
- `go vet ./domain/... ./infrastructure/...` — clean.
- `gofmt -l domain/graph/event.go domain/graph/repository.go domain/graph/session.go infrastructure/graph/sqlite_client.go infrastructure/graph/sqlite_repository.go` — prints nothing.
