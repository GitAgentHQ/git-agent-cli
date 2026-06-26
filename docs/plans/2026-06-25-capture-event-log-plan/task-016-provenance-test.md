# Task 016: graph provenance (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `application/provenance_service_test.go` — RED unit tests for the
  rename-aware, chronological provenance merge over a real temp SQLite db.

## BDD Scenario(s)
```gherkin
  Scenario: Provenance shows rename-aware file history including out-of-band changes
    Given file "old.go" was edited by an Event, renamed to "new.go", then changed out-of-band
    When "graph provenance new.go" runs
    Then the output lists all three changes in chronological order
    And the out-of-band change is flagged with source "unknown"
```

## What to implement

RED unit tests only. They must compile against the Task 001 contracts but FAIL
because `ProvenanceService` does not yet exist (task-017). Reference
architecture.md "`graph provenance <file>` → Provenance View" for the merge rules
and best-practices.md §1.4 (File Blob Ref vs Redaction Digest).

### Provenance merge — `provenance_service_test.go`

Use a real temp SQLite db (`t.TempDir()` + `NewSQLiteClient` + `InitSchema`) and a
fake/seeded graph state so the merge logic is the unit under test. Seed the chain
and projections to reproduce the scenario: an `events` row touching `old.go`
(via `event_files`), a `renames` entry mapping `old.go`→`new.go`, and an
`source="unknown"`, `kind="out-of-band"` Event touching `new.go`.

- `TestProvenance_RenameAwareChronologicalMerge`: querying provenance for
  `new.go` returns three rows in ascending chain order (`seq`): the original edit
  on `old.go`, the rename, and the out-of-band change — proving the service
  reuses `ResolveRenames` to fold the pre-rename path's history into the queried
  path (architecture.md "graph provenance": "rename-aware (`ResolveRenames`)
  merge of (a) Event-log changes for the file via `event_files`, and (b)
  Out-of-Band Events").
- `TestProvenance_OutOfBandRowFlagged`: the out-of-band row in the result is
  flagged with `source == "unknown"` (Glossary: Out-of-Band Event); observed
  rows carry their real source (`"claude-code"`).
- `TestProvenance_RowFields`: each provenance row carries `{seq, when,
  who(source), tool, before_blob→after_blob, linked_commit?}` per architecture.md;
  assert `before_blob`/`after_blob` File Blob Refs are populated from
  `event_files` for the observed change.
- Construct via the task-017 constructor (e.g. `NewProvenanceService(repo)`); the
  call site does not yet exist, so the tests fail to build/run RED.

Isolate from network/LLM — provenance is a pure read over the Event Log +
Projections; only the real SQLite driver (infra under test via the repo) and
the in-repo `ResolveRenames` seam are exercised, no subprocess git is needed
beyond what the repo already provides for seeding.

## Steps
1. Read architecture.md "`graph provenance <file>` → Provenance View" for the
   exact merge inputs (`event_files` + Out-of-Band Events), the rename-aware
   reuse of `ResolveRenames`, and the per-row field set.
2. Seed a temp SQLite db with the old.go-edit → rename → out-of-band-on-new.go
   fixture using the repository write methods + a direct `events`/`event_files`
   seed helper.
3. Write the three tests above referencing the `ProvenanceService` constructor
   and result type from task-017.
4. Run the package tests; confirm each fails because `ProvenanceService` is
   undefined, not for unrelated compile errors.

## Verification
- `go test ./application/...` — RED: tests fail to build/run because
  `ProvenanceService` (and its constructor/result type) does not exist yet.
  Confirm each failure cites the missing provenance symbol, not unrelated errors.
