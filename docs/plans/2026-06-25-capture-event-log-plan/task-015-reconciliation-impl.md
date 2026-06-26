# Task 015: Out-of-band reconciliation (impl)

**type**: impl
**depends-on**: ["014", "003", "013"]

## Files
- create: `application/reconcile_service.go` — `ReconcileService` (Blind-Spot net), reusing `GraphGitClient.DiffNameOnly`/`HashObject`
- modify: hook points — `graph rebuild` / `EnsureIndex` (before serving queries) and commit time alongside `GraphActionLinker.LinkActionsToCommit`

## BDD Scenario(s)
```gherkin
Scenario: An out-of-band human edit is reconciled as source unknown
  Given the working tree changed with no corresponding capture Event
  When the Reconciliation pass runs
  Then an Out-of-Band Event is appended with source "unknown", kind "out-of-band"
  And it carries before_blob and after_blob File Blob Refs
  And it is chained into the same Event Log (not forged as an observed Event)

Scenario: Reconciliation only covers the unexplained residual
  Given a file changed by a captured Edit Event
  And another file changed with no Event
  When the Reconciliation pass runs
  Then only the second file produces an Out-of-Band Event
```

## What to implement

`ReconcileService`, the cold-path Blind-Spot net that reintroduces diff-based
detection as a fallback only (architecture.md "Reconciliation"). It reuses
`GraphGitClient.DiffNameOnly`/`HashObject` — the exact calls removed from the hot
path in the capture-service rewrite. Signatures only — NO bodies.

```go
// application/reconcile_service.go
type ReconcileService struct {
    repo graph.GraphRepository
    git  graph.GraphGitClient
}

func NewReconcileService(repo graph.GraphRepository, git graph.GraphGitClient) *ReconcileService

type ReconcileResult struct {
    OutOfBandAppended int
    // ...residual files reconciled
}

func (s *ReconcileService) Reconcile(ctx context.Context) (ReconcileResult, error)
```

Residual-detection flow (prose — architecture.md "Reconciliation"):

1. **Candidate set.** `git.DiffNameOnly(ctx)` → working-tree files that changed
   (apply the same tooling-path exclusion the old capture used).
2. **State implied by the log.** Compute, per candidate file, the last-known-after
   blob from the Event Log replayed to HEAD (the latest `event_files.after_blob` for
   that path, rename-resolved). For each candidate, `git.HashObject(ctx, file)` gives
   the current blob.
3. **Unexplained residual.** A file is residual only if its current blob diverges
   from the state the log accounts for **and** it is not covered by a recent Event
   (skip files already explained by a captured Edit/Write/MultiEdit — no
   double-counting hook-captured edits).
4. **Append Out-of-Band Events.** For each residual file, build
   `EventRecord{Source:"unknown", Kind:"out-of-band", ToolName:"external-edit"}`
   carrying `before_blob` = last-known-after, `after_blob` = current, and append it
   via `repo.AppendEvent` so it is hash-chained into the **same** Event Log
   (tamper-evident and replayable; not forged as an observed Event). `event_files`
   rows carry the File Blob Refs.

**Hook points:** (1) `graph rebuild` / `EnsureIndex` before serving queries;
(2) at commit time alongside `GraphActionLinker.LinkActionsToCommit`, so every byte
that reaches a commit is attributable to either an observed Event or an explicit
Out-of-Band Event. Wire the call at these two seams (Composition Root / existing
ensure-index and commit paths); no business logic added to `cmd/`.

Reference: architecture.md "Reconciliation (Blind-Spot net)", "Integration Points";
best-practices.md §1.4 (File Blob Ref), §2.3 (cold-path Enrichment).

## Steps
1. Create `application/reconcile_service.go` with the struct, constructor,
   `ReconcileResult`, and `Reconcile` signature (no body) plus private
   residual-detection helper signatures.
2. Implement candidate → log-implied-state → residual → append-Out-of-Band flow,
   reusing `DiffNameOnly`/`HashObject` and `AppendEvent`.
3. Wire `Reconcile` at the `graph rebuild`/`EnsureIndex` seam and the commit-time
   seam next to `LinkActionsToCommit`.
4. Run `gofmt -w ./...` and `go build ./...`.

## Verification
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/... -run TestReconcileService` — GREEN (Task 014 tests pass)
- `go build ./...` — succeeds
- `gofmt -l application/reconcile_service.go` — prints nothing (clean)
