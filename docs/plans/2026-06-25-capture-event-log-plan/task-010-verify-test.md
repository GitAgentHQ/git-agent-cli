# Task 010: graph verify chain integrity (test)

**type**: test
**depends-on**: ["001"]

## Files
- create: `infrastructure/graph/verify_chain_test.go` — RED tests that build a real
  hash-chained `events` log in a temp SQLite db, mutate rows directly to simulate
  each tamper, and assert `VerifyChain` returns the correct `VerifyResult.status`
  and first `ChainBreak.kind`.
- create: `cmd/graph_verify_test.go` — RED test asserting `graph verify` exits 4 on a
  broken chain (exit-code contract; uses an in-process command runner or e2e-style
  subprocess consistent with existing `cmd` tests).

These are RED tests only. The verify algorithm and command land in task-011; until
then these must fail to compile or assert, observed failing for the right reason
(`VerifyChain` / `VerifyResult` / `ChainBreak` / the `graph verify` command do not
exist yet), per the BDD Iron Law in `best-practices.md` §3.

The temp-db is the test double for storage (the project's `setupCaptureTest` pattern:
real `modernc.org/sqlite` in `t.TempDir()`); no git or network is touched.

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

Failing tests for the audit surface. Reference: `architecture.md` §"Audit Surface"
classification table (the four `ChainBreak.kind` values and the invariant each
detects) and §"Exit Codes" (code **4** = chain integrity failure).

Each test builds a valid chain by appending real Events through the task-003 append
path (so `prev_hash`/`this_hash`/`seq` are genuine), then mutates `graph.db` with
raw SQL to simulate one tamper class, then calls `repo.VerifyChain(ctx)` and asserts
`VerifyResult`. Use `SQLiteRepository.Client()` for raw queries (per the existing
"used by tests to run raw queries" comment).

Asserted shape — these types are defined canonically in `task-001`
(`domain/graph/event.go`), which is the source of truth. Mirror its field types
exactly (note `EventsTotal int64` and the typed `Kind ChainBreakKind`):

```go
// VerifyResult is the outcome of walking the Event Log chain.
type VerifyResult struct {
    Status         string      // "ok" | "broken"
    EventsTotal    int64
    EventsVerified int64
    FirstBreak     *ChainBreak // nil when Status == "ok"
}

type ChainBreak struct {
    Seq              int64
    EventID          string
    Kind             ChainBreakKind // ROW_EDITED | ROW_DELETED | ROW_INSERTED | ROW_REORDERED
    ExpectedThisHash string
    StoredThisHash   string
}
```

### Test cases

1. **Untouched chain (3 Events)** — `VerifyChain` → `Status == "ok"`,
   `FirstBreak == nil`, `EventsVerified == EventsTotal == 3`. The `graph verify`
   command exits 0.
2. **Edited Event** — `UPDATE events SET payload_raw = ... WHERE seq = 2` (no
   re-chaining) → `Status == "broken"`, `FirstBreak.Seq == 2`,
   `FirstBreak.Kind == "ROW_EDITED"`; command exits 4.
3. **Deleted Event (gap)** — build 5 Events, `DELETE FROM events WHERE seq = 3` →
   `Status == "broken"`, `FirstBreak.Kind == "ROW_DELETED"` at the gap; command
   exits 4.
4. **Inserted Event** — build 3 Events, `INSERT` an extra row not reachable from the
   genesis linkage walk → `FirstBreak.Kind == "ROW_INSERTED"`.
5. **Reordered Events** — build 3 Events whose self-hashes each still recompute to
   their stored `this_hash`, but whose `prev_hash` linkage no longer follows `seq`
   order (swap rows' `seq` or `prev_hash` so linkage order ≠ seq order) →
   `FirstBreak.Kind == "ROW_REORDERED"`.

### Exit-code test (`cmd/graph_verify_test.go`)

Assert `graph verify` exits 0 on the clean chain and **4** on a broken chain (the
edited-Event fixture), matching `pkg/errors` exit-code conventions.

## Steps

1. Add `infrastructure/graph/verify_chain_test.go`: a helper that opens a temp-db
   repo (reuse the `NewSQLiteClient`/`NewSQLiteRepository`/`InitSchema` pattern) and
   appends N valid Events; one sub-test per tamper class mutating rows via
   `repo.Client()` raw SQL, then asserting `VerifyResult`.
2. Add `cmd/graph_verify_test.go` asserting the exit-code contract (0 clean, 4
   broken), consistent with how existing `cmd`/`e2e` tests assert exit codes.
3. Run the test base and confirm RED (missing `VerifyChain`/`VerifyResult`/
   `ChainBreak`/`graph verify`).

## Verification

- `go test ./application/... ./domain/... ./infrastructure/... ./cmd/... ./e2e/...`
  — expected **RED**: tests fail to compile or assert because `VerifyChain`,
  `VerifyResult`, `ChainBreak`, and the `graph verify` command are not implemented.
  The failure must name those missing symbols, not an unrelated error.
