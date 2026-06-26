# Task 013: Projections & rebuild (impl)

**type**: impl
**depends-on**: ["012", "003", "011"]

## Files
- create: `application/projection_service.go` — `ProjectionRebuilder` (the Replay engine)
- create: `cmd/graph_rebuild.go` — `graph rebuild` Cobra command wiring (Composition Root only, no business logic)

## BDD Scenario(s)
```gherkin
Scenario: Projections rebuild deterministically from the Event Log
  Given a log of ten chained Events across two sessions
  And the sessions, actions, and action_modifies projections are dropped
  When "graph rebuild" runs
  Then the projections are reconstructed solely from the Event Log
  And a second rebuild produces byte-identical projections

Scenario: Concurrent agents are attributed to separate sessions from one chain
  Given two agents capture interleaved Events with different instance_id values
  When the Events are appended to the single shared chain
  And the session projection is built
  Then each instance_id maps to its own session
  And the chain remains a single unforked sequence

Scenario: Rebuild refuses on a broken chain
  Given an Event Log with a ChainBreak
  When "graph rebuild" runs
  Then it reports the break and refuses to rebuild projections
```

## What to implement

`ProjectionRebuilder`, the cold-path Replay engine that regenerates the derived
Projections solely from the append-only Event Log. Signatures only — NO bodies.

```go
// application/projection_service.go
type ProjectionRebuilder struct {
    repo graph.GraphRepository
    git  graph.GraphGitClient
}

func NewProjectionRebuilder(repo graph.GraphRepository, git graph.GraphGitClient) *ProjectionRebuilder

func (r *ProjectionRebuilder) Rebuild(ctx context.Context) error
```

Fold logic (prose — architecture.md "Projections & Rebuild"):

1. **Verify first.** Call `repo.VerifyChain(ctx)` (from Task 011). If the result is
   not `ok`, return an error that reports the first `ChainBreak` and refuse — make no
   writes to any Projection table.
2. **Reset derived tables.** Truncate `sessions`, `actions`, `action_modifies`,
   `action_produces`, `event_files`. (Repository gains a reset/truncate seam;
   `events` is never touched — append-only.)
3. **Stream and fold.** `repo.StreamEvents(ctx, 0)` in `seq` order, accumulating:
   - **sessions** — group consecutive Events by `(source, instance_id)`; close a
     session and open a new one for the same key when the gap between consecutive
     Events for that key exceeds `sessionTimeoutMins` (`capture_service.go:11`).
     This reproduces today's `GetActiveSession` semantics deterministically. Because
     `instance_id` = Claude `session_id`, sessions map 1:1 to Claude sessions, and
     interleaved `instance_id`s on one chain yield separate sessions without forking
     the chain.
   - **actions** — one row per Event; `sequence` = per-session running counter
     derived from the global `seq` order (reproduces `CreateActionBatch`'s
     `MAX(sequence)+1` deterministically). `timestamp` comes from the Event's
     `RecordedAt` field, never wall-clock.
   - **action_modifies / event_files** — derive file paths and old/new content from
     `tool_input` in `payload_raw` (Edit/Write → `file_path` + `old_string`/
     `new_string`; MultiEdit → each edit). Compute additions/deletions from the
     payload content; compute File Blob Refs (`before_blob`/`after_blob`) here in the
     cold path via `git.HashObject` (the calls removed from the hot path).
   - **action_produces** — not built here; preserved via the existing
     `GraphActionLinker.LinkActionsToCommit` seam at commit time.
4. **Determinism rules.** Ordering and timestamps derive **solely** from Event
   fields (`seq`, `recorded_at`, `source`, `instance_id`, …) and chain order — no
   wall-clock reads, no map-iteration ordering. Iterate maps via sorted keys.

`Timeline` (`sqlite_repository.go:797`) and `UnlinkedActionsForFiles`
(`sqlite_repository.go:944`) are reused unchanged as Projection consumers — their
input tables are now Projections produced by this fold.

`cmd/graph_rebuild.go` wires `NewProjectionRebuilder(repo, git)` and calls
`Rebuild`; no business logic in `cmd/`.

Reference: architecture.md "Projections & Rebuild", "Schema" (`event_files`),
"Reconciliation"; best-practices.md §2.3 (Enrichment / Rebuild — watermark, full
Replay only on schema change/corruption).

## Steps
1. Create `application/projection_service.go` with the struct, constructor, and
   `Rebuild` signature (no body) and any private fold-helper signatures.
2. Add the Projection-reset seam on `GraphRepository`/`SQLiteRepository` if not
   already present (truncate derived tables; never `events`).
3. Implement the verify-first → reset → stream-fold pipeline.
4. Create `cmd/graph_rebuild.go` wiring the rebuilder into the `graph` command tree.
5. Run `gofmt -w ./...` and `go build ./...`.

## Verification
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/... -run TestProjectionRebuilder` — GREEN (Task 012 tests pass)
- `go build ./...` — succeeds
- `gofmt -l application/projection_service.go cmd/graph_rebuild.go` — prints nothing (clean)
