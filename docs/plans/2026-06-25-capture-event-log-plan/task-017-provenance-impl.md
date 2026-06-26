# Task 017: graph provenance (impl)

**type**: impl
**depends-on**: ["016", "013", "015"]

## Files
- create: `application/provenance_service.go` — `ProvenanceService` (rename-aware
  chronological merge of `event_files` changes + Out-of-Band Events).
- create: `cmd/graph_provenance.go` — Cobra wiring for `graph provenance <file>`.

## BDD Scenario(s)
```gherkin
  Scenario: Provenance shows rename-aware file history including out-of-band changes
    Given file "old.go" was edited by an Event, renamed to "new.go", then changed out-of-band
    When "graph provenance new.go" runs
    Then the output lists all three changes in chronological order
    And the out-of-band change is flagged with source "unknown"
```

## What to implement

GREEN: make task-016 pass. Reference architecture.md "`graph provenance <file>` →
Provenance View" (the merge spec) and NFR2 (Clean-Architecture placement —
orchestration in `application/`, wiring only in `cmd/`). Reuses the
`ResolveRenames` seam and the `event_files` join populated by the projection
rebuild (task-013) and reconciliation (task-015).

Signatures only — NO bodies.

### `application/provenance_service.go`

```go
package application

// ProvenanceRow is one chronological change to a file in the Provenance View.
type ProvenanceRow struct {
    Seq          int64
    When         int64       // recorded_at, unix seconds
    Who          string      // EventSource: claude-code | cursor | human | unknown
    Tool         string
    BeforeBlob   string      // File Blob Ref
    AfterBlob    string      // File Blob Ref
    ChangeKind   string      // A|M|D|R
    LinkedCommit string      // from action_produces, empty if none
    OutOfBand    bool        // true when Who == "unknown"
}

// ProvenanceView is the chronological, rename-aware history for one file.
type ProvenanceView struct {
    File string
    Rows []ProvenanceRow
}

// ProvenanceService builds a rename-aware, chronological change history for a
// file from the Event Log (event_files) plus Out-of-Band Events.
type ProvenanceService struct {
    repo graph.GraphRepository
}

func NewProvenanceService(repo graph.GraphRepository) *ProvenanceService

// Provenance returns the chronological Provenance View for filePath.
func (s *ProvenanceService) Provenance(ctx context.Context, filePath string) (*ProvenanceView, error)
```

Query/merge flow (prose, no bodies):

1. Resolve the file's prior identities via `repo.ResolveRenames(ctx, filePath)`
   so a queried path (`new.go`) inherits the history of its pre-rename paths
   (`old.go`).
2. Read `event_files` rows for the resolved path set, joined to their `events`
   row, to obtain `{seq, recorded_at, source, tool_name, before_blob,
   after_blob, change_kind}` — observed and out-of-band Events alike live in the
   same `events` table, so one ordered read covers both (a) and (b) from
   architecture.md.
3. Attach `linked_commit` from `action_produces` for the touched path where
   present (architecture.md "graph provenance" row shape `linked_commit?`).
4. Order rows by global `seq` ascending; set `OutOfBand = (Who == "unknown")` so
   out-of-band rows are flagged (Glossary: Out-of-Band Event).

### `cmd/graph_provenance.go`

- A Cobra subcommand under the existing `graph` parent: `provenance <file>`.
- Wiring only (NFR2): open the repo, construct `NewProvenanceService(repo)`, call
  `Provenance`, render the rows (chronological, out-of-band rows visibly
  flagged). No business logic in `cmd/`.

## Steps
1. Read architecture.md "graph provenance" and NFR2.
2. Add `ProvenanceRow`/`ProvenanceView`/`ProvenanceService` signatures to
   `application/provenance_service.go`; implement the resolve→read→merge→order
   flow.
3. Add `cmd/graph_provenance.go` wiring the service under the `graph` parent.
4. Run task-016; iterate to GREEN. Run `go build ./...` and `gofmt -l`.

## Verification
- `go test ./application/... -run TestProvenance` — GREEN (task-016 passes).
- `go build ./...` — succeeds.
- `gofmt -l application/provenance_service.go cmd/graph_provenance.go` — prints
  nothing (clean).
- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — no regressions in previously passing tasks.
