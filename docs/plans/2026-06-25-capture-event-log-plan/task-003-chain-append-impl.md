# Task 003: GREEN — SHA256 hasher and atomic chain append

**type**: impl
**depends-on**: ["002"]

## Files
- create: `infrastructure/graph/sha256_hasher.go` — `EventHasher` impl + canonical-form construction
- modify: `infrastructure/graph/sqlite_repository.go` — implement `AppendEvent`, `HeadHash`, `StreamEvents` (replace the Task 001 stubs)

## BDD Scenario(s)
```gherkin
  Scenario: The first Event chains from Genesis
    Given the Event Log is empty
    When the first capture is appended
    Then the Event seq is 1
    And the Event prev_hash is the Genesis value (64 hex zeros)
    And the Event this_hash covers seq, recorded_at, source, tool_name, and the payload bytes

  Scenario: Ground truth is retained instead of reconstructed
    Given an Edit payload whose tool_input contains old_string and new_string
    When capture is invoked
    Then the Event is stored without invoking git diff or git hash-object on the hot path

  Scenario: Append-only is enforced
    Given the Event Log contains Events
    When any code path attempts an UPDATE or DELETE on the events table
    Then the attempt fails a test guard
    And no production path issues such a statement
```

## What to implement

Make Task 002 GREEN. Signatures only below — implementation bodies are written
during execution.

### `infrastructure/graph/sha256_hasher.go`

```go
type SHA256Hasher struct{}

func NewSHA256Hasher() *SHA256Hasher

// Hash implements graph.EventHasher.
func (h *SHA256Hasher) Hash(prevHash string, e graph.EventRecord) string
```

Canonical-form construction per architecture.md "Hash Chain" / best-practices.md §1.3:

- `this_hash = SHA256( prev_hash ‖ "\n" ‖ CanonicalForm(e) )`.
- `CanonicalForm(e)` = `chain_version_byte` ‖ `le_uint64(seq)` ‖
  `le_uint64(recorded_at)` ‖ `len_prefixed(source)` ‖ `len_prefixed(instance_id)` ‖
  `len_prefixed(kind)` ‖ `len_prefixed(tool_name)` ‖ `le_int(exit_code_or_sentinel)`
  ‖ `sha256(payload_raw)`.
- Fixed-order, length-prefixed scalars (no JSON ordering/whitespace ambiguity); the
  variable payload is covered by hashing its **exact stored bytes** — never
  re-serialized. `chain_version_byte` freezes v1. Add an unexported helper for
  length-prefixed writes and the exit-code sentinel (for `ExitCode == nil`).

### `infrastructure/graph/sqlite_repository.go`

```go
func (r *SQLiteRepository) AppendEvent(ctx context.Context, e graph.EventRecord) (graph.EventRecord, error)
func (r *SQLiteRepository) HeadHash(ctx context.Context) (string, error)
func (r *SQLiteRepository) StreamEvents(ctx context.Context, sinceSeq int64) (graph.EventCursor, error)
```

- `AppendEvent` is the **only writer** into `events`. Critical section per
  best-practices.md §2.1/§2.2: one `BEGIN IMMEDIATE` transaction that reads the
  current head `prev_hash` → computes `this_hash` (via the injected/owned
  `EventHasher` — wire the hasher into the repo or accept it on the call path
  consistent with Task 001) → single `INSERT` that lets SQLite assign `seq`
  (AUTOINCREMENT) and stores `prev_hash`/`this_hash`. Read-head and insert must be
  the same transaction so two writers cannot read the same head and fork the chain.
  Genesis `prev_hash` = 64 hex zeros for the first row.
- `HeadHash` returns the current chain head (`this_hash` of `MAX(seq)`), or the
  Genesis value on an empty log, in a read-only query.
- `StreamEvents(sinceSeq)` returns an `EventCursor` iterating `events` in `seq`
  order where `seq > sinceSeq`. Read-only; safe under WAL.
- Do **not** add any `UPDATE`/`DELETE`/`INSERT OR REPLACE` on `events` (keeps the
  Task 002 append-only guard green).

Note: `seq` is assigned by AUTOINCREMENT, but `CanonicalForm` folds `seq` in as a
field value — resolve the ordering (e.g. reserve/assign `seq` inside the txn before
hashing, or hash with the to-be-assigned `seq`) so the stored `this_hash` matches
what `verify` later recomputes. Document the chosen approach in code.

## Steps
1. Write `sha256_hasher.go` with `NewSHA256Hasher` and `Hash`, plus canonical-form helpers.
2. Replace the `AppendEvent` stub with the `BEGIN IMMEDIATE` read-head → hash → insert critical section.
3. Replace the `HeadHash` and `StreamEvents` stubs with read-only implementations.
4. Run the Task 002 package tests until green.

## Verification
- `go test ./infrastructure/...` — Task 002 tests pass GREEN (determinism, genesis seq=1 + 64-zero prev_hash, prev chains to prior this_hash, head hash, append-only guard).
- `go build ./...` — compiles.
- `gofmt -l infrastructure/graph/sha256_hasher.go infrastructure/graph/sqlite_repository.go` — prints nothing.
