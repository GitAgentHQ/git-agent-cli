# Task 011: graph verify chain integrity (impl)

**type**: impl
**depends-on**: ["010", "003"]

## Files
- modify: `infrastructure/graph/sqlite_repository.go` — implement
  `VerifyChain(ctx) (graph.VerifyResult, error)`: a linkage walk from Genesis
  compared against `seq` order, classifying the first `ChainBreak`.
- create: `application/verify_service.go` — `VerifyService` orchestrating
  `repo.VerifyChain` and shaping the `VerifyResult` for the command layer.
- create: `cmd/graph_verify.go` — the `graph verify` Cobra subcommand; exit **4** on
  any break, `--json` for machine-readable output.

This makes task-010 GREEN.

## BDD Scenario(s)

```gherkin
  Scenario: graph verify passes on an untouched chain
    Given three chained Events exist
    When "graph verify" runs
    Then it reports status "ok"
    And it exits 0

  Scenario: graph verify detects an edited Event
    Given three chained Events exist
    And the payload_raw of the second Event is edited directly in graph.db without re-chaining
    When "graph verify" runs
    Then it reports status "broken"
    And the first ChainBreak has seq 2 and kind "ROW_EDITED"
    And it exits 4

  Scenario: graph verify detects a deleted Event via sequence gap
    Given five chained Events with seq 1..5
    And the Event at seq 3 is deleted from graph.db
    When "graph verify" runs
    Then it reports status "broken"
    And the first ChainBreak kind is "ROW_DELETED" at the gap
    And it exits 4

  Scenario: graph verify detects an inserted Event
    Given three chained Events exist
    And an extra row is inserted that is unreachable from the Genesis linkage walk
    When "graph verify" runs
    Then the first ChainBreak kind is "ROW_INSERTED"

  Scenario: graph verify detects reordered Events
    Given three chained Events whose self-hashes each recompute to their stored this_hash
    But whose prev_hash linkage no longer follows seq order
    When "graph verify" runs
    Then the first ChainBreak kind is "ROW_REORDERED"
```

## What to implement

Reference: `architecture.md` §"Audit Surface" (classification table + `VerifyResult`
shape), §"Hash Chain" (Canonical Form, Genesis = 64 hex zeros, `EventHasher`), and
§"Exit Codes" (code **4**). `best-practices.md` pitfall 12 (deletion is only visible
as a `seq` gap — `verify` must check `seq` continuity explicitly) and §1.2
(tamper-evident, not tamper-proof — do not oversell). NO function bodies — prose
algorithm and signatures only.

### Verify algorithm (`VerifyChain`, prose)

Read-only; safe under WAL during an active writer (`architecture.md` §"Audit
Surface"). Stream `events ORDER BY seq`. For each row:

1. **Recompute `this_hash`** via the same `EventHasher.Hash(prev_hash, e)` used at
   append (task-003), over the Canonical Form. Compare to the stored `this_hash`.
2. **Track `expected_prev`**: the previous row's stored `this_hash` (Genesis =
   64 hex zeros before the first row).
3. **Linkage walk**: independently follow `prev_hash` pointers starting from the
   Genesis row (the row whose `prev_hash` is 64 hex zeros) forward via
   `this_hash → prev_hash` links, recording the order links are traversed.
4. **Check `seq` continuity** explicitly: `seq` values must be contiguous from the
   first row (gap ⇒ a row was deleted).

Classify the **first** failing invariant per the table:

| First failing invariant | `ChainBreak.kind` |
|---|---|
| self-hash mismatch, linkage intact, seq contiguous | `ROW_EDITED` |
| `seq` gap + linkage walk terminates early (missing link target) | `ROW_DELETED` |
| table row not reachable from the genesis linkage walk | `ROW_INSERTED` |
| every self-hash recomputes but linkage order ≠ seq order | `ROW_REORDERED` |

Populate `FirstBreak` with `{Seq, EventID, Kind, ExpectedThisHash, StoredThisHash}`.
`Status = "ok"` with `FirstBreak == nil` when all invariants hold;
`EventsVerified` counts rows confirmed before the first break (all rows when ok).

Signature (matches `domain/graph/repository.go` from task-001):

```go
func (r *SQLiteRepository) VerifyChain(ctx context.Context) (graph.VerifyResult, error)
```

### Application service (`application/verify_service.go`)

```go
type VerifyService struct {
    repo graph.GraphRepository
}

func NewVerifyService(repo graph.GraphRepository) *VerifyService

// Verify runs the chain integrity check and returns the result. Orchestration
// only — the walk/classification lives in the repository (infrastructure).
func (s *VerifyService) Verify(ctx context.Context) (graph.VerifyResult, error)
```

### Command (`cmd/graph_verify.go`)

Cobra subcommand under the existing `graph` parent. Wires the repository +
`VerifyService`, prints a human summary by default and a JSON `VerifyResult` under
`--json`. On `VerifyResult.Status == "broken"`, return the error type that maps to
**exit code 4** (extend `pkg/errors` with a chain-integrity sentinel/exit code if
one does not already exist; per the global guideline, this is a 1:1 mapping, not a
new abstraction). Read-only command — never mutates `events`.

## Steps

1. Implement `VerifyChain` in `infrastructure/graph/sqlite_repository.go` per the
   algorithm above (recompute via `EventHasher`, linkage walk, seq-continuity check,
   first-break classification).
2. Add `application/verify_service.go` with `VerifyService` orchestration.
3. Add `cmd/graph_verify.go`: wire and register the subcommand, `--json` flag, exit
   4 on break (add the exit-code mapping in `pkg/errors` if missing).
4. Run task-010 and confirm GREEN; run `go build ./...` and `gofmt -l`.

## Verification

- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — task-010 tests pass (GREEN), including the exit-4-on-break assertion.
- `go build ./...` — succeeds.
- `gofmt -l infrastructure/graph/sqlite_repository.go application/verify_service.go cmd/graph_verify.go`
  — prints nothing (formatting clean).
