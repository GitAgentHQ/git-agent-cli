# Task 014: Out-of-band reconciliation (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `application/reconcile_service_test.go` — RED tests for `ReconcileService`

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

Failing tests for `ReconcileService` — the Blind-Spot net (architecture.md
"Reconciliation"). The service compares the current working-tree file hashes
against the state implied by Replaying Events to HEAD, and for any **unexplained
residual** file (changed but not covered by recent Events) appends a synthetic
`EventRecord{Source:"unknown", Kind:"out-of-band", ToolName:"external-edit"}` into
the **same hash-chained Event Log**, carrying `before_blob` = last-known-after and
`after_blob` = current. Files already covered by recent Events are skipped (no
double-counting).

Tests drive the real `SQLiteRepository` over a temp `graph.db` (so the appended
Out-of-Band Event is really hash-chained and re-verifiable) plus a fake
`GraphGitClient` exposing `DiffNameOnly` (the residual candidate set) and
`HashObject` (current File Blob Refs) — the two calls removed from the hot path and
re-introduced here.

Constructor/method under test (signatures only — NO bodies):

```go
// application/reconcile_service.go (created in Task 015)
type ReconcileService struct { /* repo graph.GraphRepository; git graph.GraphGitClient */ }

func NewReconcileService(repo graph.GraphRepository, git graph.GraphGitClient) *ReconcileService

func (s *ReconcileService) Reconcile(ctx context.Context) (ReconcileResult, error)
```

Tests to write (must compile and FAIL because `reconcile_service.go` does not yet
exist):

1. `TestReconcileService_AppendsUnknownOutOfBandEvent` — seed the Event Log so that
   one file's last-known-after blob is recorded, then have the fake `DiffNameOnly`
   report that file changed with a different current `HashObject` (a working-tree
   change with no corresponding capture Event). Call `Reconcile`. Assert exactly one
   new Event was appended with `Source == "unknown"`, `Kind == "out-of-band"`,
   `ToolName == "external-edit"`, non-empty `before_blob` and `after_blob` File Blob
   Refs (in `event_files`), and that `VerifyChain` still reports `ok` (it was chained
   into the same log, not forged as an observed Event — it has a valid
   `prev_hash`/`this_hash`).

2. `TestReconcileService_OnlyUnexplainedResidual` — seed a captured Edit Event for
   `a.go` (so its current working-tree hash matches the state implied by Replay),
   and have `DiffNameOnly` report both `a.go` and `b.go` changed where `b.go` has no
   Event. Call `Reconcile`. Assert that **only** `b.go` produces an Out-of-Band
   Event, and `a.go` does not (it is explained by the recent Event — architecture.md
   "Reconcile only the unexplained residual ... to avoid double-counting
   hook-captured edits").

## Steps
1. Add a fake `graph.GraphGitClient` whose `DiffNameOnly` and `HashObject` are
   programmable per file path.
2. Reuse the temp-db Event-seeding helper from Task 012 (real `SQLiteRepository`).
3. Write the two tests above against `NewReconcileService(repo, gitFake)`, asserting
   appended Event fields, `event_files` File Blob Refs, residual-only coverage, and
   a clean `VerifyChain` afterward.

Reference: architecture.md "Reconciliation (Blind-Spot net)", "Schema"
(`event_files` File Blob Refs); best-practices.md §1.4 (File Blob Ref vs Redaction
Digest), §3 (Testing).

## Verification
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/... -run TestReconcileService` — RED: tests compile and fail because `ReconcileService`/`Reconcile` is not yet implemented (failing for the right reason).
